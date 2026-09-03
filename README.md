# GopherWrap

Turn Redis commands into a `gopher://` URL for SSRF testing. Every command
is terminated with CRLF automatically, then the payload is percent-encoded
and wrapped — nothing to hand-assemble.

```console
$ gopherwrap

  GopherWrap — Redis over gopher:// (SSRF)
  ┃ Host                          ┃ Port                    ┃ Payload
  ┃ Redis the SSRF point can …    ┃ Redis listens here …    ┃ SET test hello
  ┃ > 127.0.0.1                   ┃ > 6379                  ┃ [one command per line]
  …
```

## Usage

```console
# interactive form
gopherwrap

# one-shot
gopherwrap -host 10.0.0.5 -port 6379 -payload 'SET test hello'
gopherwrap -f payload.txt                         # read commands from a file
echo 'AUTH secret
CONFIG GET dir' | gopherwrap                       # payload from stdin
gopherwrap -d                                      # also print double-encoded form
```

| flag        | meaning                                              |
|-------------|------------------------------------------------------|
| `-host`     | Redis host (default `127.0.0.1`, IPv6 ok)            |
| `-port`     | Redis port (default `6379`)                          |
| `-payload`  | commands inline, one per line                        |
| `-file`/`-f`| read the payload from a file                         |
| `-d`        | print the double-encoded variant on the next line    |



## Install

```console
go install -v github.com/mfkrypt/gopherwrap@latest
```

For local development the module builds as-is:

```console
go build -o gopherwrap .
```
