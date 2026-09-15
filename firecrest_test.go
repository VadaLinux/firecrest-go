package firecrest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "cluster-a", NewStaticTokenHTTPClient(context.Background(), "test-token"))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSubmitUsesV2PathAndJSON(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/cluster-a/jobs" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("X-Machine-Name"); got != "" {
			t.Errorf("legacy header set: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"working_directory":"/work"`) {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"jobId":42}`))
	})
	id, err := client.Submit(context.Background(), SubmitRequest{Job: JobDescription{Script: "echo hi", WorkingDirectory: "/work"}})
	if err != nil {
		t.Fatal(err)
	}
	if id != "42" {
		t.Fatalf("job ID = %q", id)
	}
}

func TestJobStatusUsesMetadataEndpoint(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/cluster-a/jobs/42/metadata" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"jobs":[{"jobId":"42","standardOutput":"/work/out","standardError":"/work/err"}]}`))
	})
	status, err := client.JobStatus(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if status.StandardOutput != "/work/out" || status.StandardError != "/work/err" {
		t.Fatalf("status = %#v", status)
	}
}

func TestUploadAndDownload(t *testing.T) {
	var uploaded bool
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/filesystem/cluster-a/ops/upload":
			if r.URL.Query().Get("path") != "/work" {
				t.Errorf("upload path = %q", r.URL.Query().Get("path"))
			}
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			body, _ := io.ReadAll(file)
			if header.Filename != "input.txt" || string(body) != "hello" {
				t.Errorf("file = %q %q", header.Filename, body)
			}
			uploaded = true
			w.WriteHeader(http.StatusNoContent)
		case "/filesystem/cluster-a/ops/download":
			if r.URL.Query().Get("path") != "/work/out.txt" {
				t.Errorf("download path = %q", r.URL.Query().Get("path"))
			}
			_, _ = w.Write([]byte("output"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	if err := client.Upload(context.Background(), "/work", "input.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Fatal("upload was not received")
	}
	body, err := client.Download(context.Background(), "/work/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != "output" {
		t.Fatalf("download = %q", got)
	}
}

func TestAPIErrorIsQueryable(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "missing", http.StatusNotFound) })
	_, err := client.JobStatus(context.Background(), "unknown")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected APIError, got %#v", err)
	}
}
