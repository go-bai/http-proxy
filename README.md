# simple and useful http proxy

## Quick Start

### Run with docker

```bash
docker run -d --net=host -e HTTP_PROXY_LISTEN_ADDR=":8888" --restart always --name http-proxy gobai/http-proxy:v0.1.6
```

### Test

if your local ip is 2.2.2.2 and your http-proxy server ip is 4.4.4.4

```bash
$ curl ip.sb
2.2.2.2
$ http_proxy=4.4.4.4:8888 curl ip.sb
4.4.4.4
```

### IP Whitelist

The ip whitelist file is located in `conf/whitelist`.

example:

```bash
127.0.0.1/32
1.1.1.1/32
```

```bash
mkdir -p http-proxy-conf
echo '127.0.0.1/32' > http-proxy-conf/whitelist
docker run -d --net=host -e HTTP_PROXY_LISTEN_ADDR=":8888" -v ${PWD}/http-proxy-conf:/conf --restart always --name http-proxy gobai/http-proxy:v0.1.6
```

## Environment Variable

| key | default |
| --- | - |
| `HTTP_PROXY_LISTEN_ADDR` | `:8888` |

## Credits

- [sobyte](https://www.sobyte.net/post/2021-09/https-proxy-in-golang-in-less-than-100-lines-of-code/)
