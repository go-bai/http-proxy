# simple and useful http proxy

## Quick Start

```bash
docker run -d -p 8888:8888 --restart always --name http-proxy gobai/http-proxy:v0.0.1
```

## Environment Variable

| key | default |
| --- | - |
| `HTTP_PROXY_LISTEN_ADDR` | `:8888` |

## Credits

- [sobyte](https://www.sobyte.net/post/2021-09/https-proxy-in-golang-in-less-than-100-lines-of-code/)
