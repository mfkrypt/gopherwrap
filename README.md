# GopherWrap

Turn Redis commands into a `gopher://` URL for SSRF testing. Every command
is terminated with CRLF, then the payload is percent-encoded and wrapped.


## Usage


| flag        | meaning                                              |
|-------------|------------------------------------------------------|
| `-h`        | Help option                                          |
| `-host`     | Redis host (default `127.0.0.1`, IPv6 ok)            |
| `-port`     | Redis port (default `6379`)                          |
| `-payload`  | commands inline, one per line                        |
| `-file`/`-f`| read the payload from a file                         |
| `-d`        | print the double-encoded variant on the next line    |


## Installation

```go
go install -v github.com/mfkrypt/gopherwrap@latest
```


### > TUI

```console
$ gopherwrap

  GopherWrap — Redis → gopher://
  ┃ Host                          ┃ Port                    ┃ Payload
  ┃ Redis the SSRF point can …    ┃ Redis listens here …    ┃ SET test hello
  ┃ > 127.0.0.1                   ┃ > 6379                  ┃ [one command per line]
  …
```


### > One-Shot

```bash
gopherwrap -host 10.0.0.5 -port 6379 -payload 'SET test hello'
gopherwrap -f payload.txt                         # read commands from a file
echo 'AUTH secret
CONFIG GET dir' | gopherwrap                       # payload from stdin
gopherwrap -d                                      # also print double-encoded form
```


## TODO:

Add templates option for MySQL, PostgreSQL, FastCGI, Memcached, Zabbix, SMTP
