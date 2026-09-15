# firecrest-go

`firecrest-go` is a small Go library and command-line client for [FirecREST v2](https://eth-cscs.github.io/firecrest-v2/). It targets the v2 path-based API: the HPC system is part of each URL, not an `X-Machine-Name` header.

It covers OAuth2 client credentials, JSON job submission, job metadata/status, and direct small-file upload/download. Large asynchronous transfers deliberately remain out of scope for this first version.

## Quick start

```go
httpClient := firecrest.NewClientCredentialsHTTPClient(ctx, firecrest.ClientCredentials{
    TokenURL: "https://keycloak.example/auth/realms/myrealm/protocol/openid-connect/token",
    ClientID: "my-client", ClientSecret: os.Getenv("CLIENT_SECRET"),
})
client, err := firecrest.NewClient("https://firecrest.example", "my-system", httpClient)
id, err := client.Submit(ctx, firecrest.SubmitRequest{Job: firecrest.JobDescription{
    Script: "#!/bin/bash\necho hello", WorkingDirectory: "/home/me",
}})
```

Every network method takes `context.Context`; cancel it to stop an in-flight request. API failures are `*firecrest.APIError`; use `errors.Is(err, firecrest.ErrUnauthorized)` or `errors.Is(err, firecrest.ErrNotFound)`.

## CLI

The CLI uses an existing bearer token, so it never puts a client secret on a command line:

```bash
export FIRECREST_URL=https://firecrest.example FIRECREST_SYSTEM=my-system FIRECREST_TOKEN=…
firecrest-go systems
firecrest-go submit -name myjob -out myjob.out -err myjob.err job.sh /home/me
firecrest-go status 12345
firecrest-go download /home/me/myjob.out ./output.txt
```

Pass `-out`/`-err` when you intend to read a job's output back. Without them the
scheduler picks the default `slurm-<jobid>.out`, and whether that file really appears
where the API reports it is up to the site's Slurm configuration — see the caveat below.

## Verification

Unit tests use `httptest`, require no running FirecREST stack, and are checked with
`go vet`, `staticcheck`, and `go test -race`.

Beyond that, this client has been **run end to end against a real FirecREST v2 server**
(`f7t-appversion: 2.6.0`) on the project's local demo stack, on 2026-09-15: OAuth2
client-credentials token, `systems`, `submit`, `status` polled to `COMPLETED`, and
`download` of the job's stdout all succeeded. What that run found is recorded in
`docs/verification-v2.md`.

**No real Alps system and no real CSCS credentials have been exercised.** The demo
stack's scheduler is a shell stand-in, not Slurm, and its tokens are not representative
of Keycloak's short-lived ones — so timing, token refresh under load, and real
scheduler states remain unverified.

## License

Apache-2.0. See `LICENSE`.

## Related work

This is the Go companion to [firecrest-agentic-workbench](https://github.com/VadaLinux/firecrest-agentic-workbench). The workbench itself is intentionally not modified by this repository.
