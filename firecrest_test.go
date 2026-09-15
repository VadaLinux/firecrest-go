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
		if !strings.Contains(string(body), `"workingDirectory":"/work"`) {
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

// FirecREST 2.6.0 returns jobId as a JSON string; the OpenAPI schema types it
// as a nullable string. The numeric form is kept working because the original
// client was written against it and it costs one branch to accept both.
func TestSubmitAcceptsStringAndNumericJobID(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"string, as FirecREST 2.6.0 replies", `{"jobId":"42"}`, "42"},
		{"number", `{"jobId":42}`, "42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(tc.body))
			})
			id, err := client.Submit(context.Background(), SubmitRequest{Job: JobDescription{Script: "echo hi", WorkingDirectory: "/work"}})
			if err != nil {
				t.Fatal(err)
			}
			if id != tc.want {
				t.Fatalf("job ID = %q, want %q", id, tc.want)
			}
		})
	}
}

func TestJobReadsSchedulerState(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/cluster-a/jobs/42" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"jobs":[{"jobId":"42","name":"j","cluster":"cluster-a","partition":"debug",
			"nodes":"node0","user":"demo","workingDirectory":"/home/demo",
			"status":{"state":"COMPLETED","stateReason":"None","exitCode":0,"interruptSignal":0}}]}`))
	})
	job, err := client.Job(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status.State != "COMPLETED" || !job.Status.Terminal() {
		t.Fatalf("job = %#v", job)
	}
	if job.Status.ExitCode == nil || *job.Status.ExitCode != 0 {
		t.Fatalf("exit code = %v", job.Status.ExitCode)
	}
	if job.WorkingDirectory != "/home/demo" || job.Partition != "debug" {
		t.Fatalf("job = %#v", job)
	}
}

// A running job must not be mistaken for a finished one, and an unrecognised
// state must keep a caller polling rather than report a wrong outcome.
func TestJobStateTerminal(t *testing.T) {
	for state, want := range map[string]bool{
		"COMPLETED": true, "FAILED": true, "CANCELLED": true, "TIMEOUT": true,
		"OUT_OF_MEMORY": true, "NODE_FAIL": true,
		"PENDING": false, "RUNNING": false, "CONFIGURING": false,
		"COMPLETING": false, "SUSPENDED": false, "": false, "SOMETHING_NEW": false,
	} {
		if got := (JobState{State: state}).Terminal(); got != want {
			t.Errorf("Terminal(%q) = %t, want %t", state, got, want)
		}
	}
}

// Systems is the one call that is not scoped to the client's system, so it must
// not acquire the /{system}/ path segment every other call carries.
func TestSystemsIsNotSystemScoped(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status/systems" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"systems":[{"name":"fakecluster"}]}`))
	})
	systems, err := client.Systems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(systems) != 1 || systems[0].Name != "fakecluster" {
		t.Fatalf("systems = %#v", systems)
	}
}

// FirecREST's error envelope is JSON, not the plain text httptest usually
// returns; APIError must surface its message rather than swallow it.
func TestAPIErrorCarriesFirecRESTEnvelope(t *testing.T) {
	const envelope = `{"errorType":"error","message":"Access token is invalid.","causedBy":null,"data":null,"user":null}`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(envelope))
	})
	_, err := client.Job(context.Background(), "42")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %#v", err)
	}
	if !strings.Contains(apiErr.Detail, "Access token is invalid.") {
		t.Fatalf("detail = %q", apiErr.Detail)
	}
}
