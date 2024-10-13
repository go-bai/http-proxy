# simple and useful http proxy

## Quick Start

### Get the latest tag

```bash
apt/yum install jq -y
TAG=`curl -s GET https://api.github.com/repos/go-bai/http-proxy/tags\?per_page=1 | jq -r '.[].name'`
echo $TAG
```

### Run with Docker

```bash
docker run --rm --net=host --name http-proxy ghcr.io/go-bai/http-proxy:$TAG
```

### custom password

```bash
docker run -d --net=host -e HTTP_PROXY_PASS="xxx" --restart always --name http-proxy ghcr.io/go-bai/http-proxy:$TAG
```

## Environment Variable

| key | default |
| --- | - |
| `HTTP_PROXY_ADDR` | `:38888` |
| `HTTP_PROXY_AUTH` | `on`    |
| `HTTP_PROXY_PASS` | ``      |

## Credits

- [sobyte](https://www.sobyte.net/post/2021-09/https-proxy-in-golang-in-less-than-100-lines-of-code/)
