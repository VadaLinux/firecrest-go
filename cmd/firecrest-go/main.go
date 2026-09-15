package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	firecrest "github.com/VadaLinux/firecrest-go"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	baseURL := fs.String("url", os.Getenv("FIRECREST_URL"), "FirecREST base URL")
	system := fs.String("system", os.Getenv("FIRECREST_SYSTEM"), "FirecREST system name")
	token := fs.String("token", os.Getenv("FIRECREST_TOKEN"), "Bearer token")
	fs.Parse(os.Args[2:])
	if *baseURL == "" || *system == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "-url, -system and -token (or matching environment variables) are required")
		os.Exit(2)
	}
	client, err := firecrest.NewClient(*baseURL, *system, firecrest.NewStaticTokenHTTPClient(context.Background(), *token))
	if err != nil {
		fatal(err)
	}
	switch os.Args[1] {
	case "submit":
		if fs.NArg() != 2 {
			usage()
		}
		script, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			fatal(err)
		}
		id, err := client.Submit(context.Background(), firecrest.SubmitRequest{Job: firecrest.JobDescription{Script: string(script), WorkingDirectory: fs.Arg(1)}})
		if err != nil {
			fatal(err)
		}
		fmt.Println(id)
	case "status":
		if fs.NArg() != 1 {
			usage()
		}
		status, err := client.JobStatus(context.Background(), fs.Arg(0))
		if err != nil {
			fatal(err)
		}
		fmt.Printf("job=%s stdout=%s stderr=%s\n", status.JobID, status.StandardOutput, status.StandardError)
	case "download":
		if fs.NArg() != 2 {
			usage()
		}
		body, err := client.Download(context.Background(), fs.Arg(0))
		if err != nil {
			fatal(err)
		}
		defer body.Close()
		out, err := os.Create(filepath.Clean(fs.Arg(1)))
		if err != nil {
			fatal(err)
		}
		_, err = io.Copy(out, body)
		closeErr := out.Close()
		if err != nil {
			fatal(err)
		}
		if closeErr != nil {
			fatal(closeErr)
		}
	default:
		usage()
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func usage() {
	fmt.Fprintln(os.Stderr, "usage: firecrest-go <submit|status|download> [flags] args")
	os.Exit(2)
}
