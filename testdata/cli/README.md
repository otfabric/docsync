# democtl

`democtl read` reads with `--url` and `--count`; `--format` and `--verbose` are global.

```sh
democtl read --url tcp://x --count 2 | jq .
democtl get -u tcp://x -v
democtl read --count 2
democtl read --nope
democtl frobnicate
democtl --help
democtl read --url "tcp://x" --format=json > out.json
FOO=1 democtl read -u <url> # comment
```

```console
$ democtl read
Error: required flag "url" not set
exit status 2
$ democtl write --value 1
```

```
democtl output 1.0
```
