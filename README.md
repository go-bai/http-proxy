# simple and useful http/socks5 proxy

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

### Install with deb

Download the deb package for your machine from the GitHub release assets:

- `http-proxy_${TAG#v}_amd64.deb` for linux amd64
- `http-proxy_${TAG#v}_arm64.deb` for linux arm64

```bash
sudo dpkg -i http-proxy_${TAG#v}_amd64.deb
sudo editor /etc/default/http-proxy
sudo systemctl enable --now http-proxy
sudo systemctl status http-proxy
```

The deb package installs:

- `/usr/bin/http-proxy`
- `/lib/systemd/system/http-proxy.service`
- `/etc/default/http-proxy`

Set `HTTP_PROXY_PASS` in `/etc/default/http-proxy` before starting the service when `HTTP_PROXY_AUTH=on`.

## Environment Variable

| key | default |
| --- | - |
| `HTTP_PROXY_ADDR` | `:38888` |
| `SOCKS5_PROXY_ADDR` | `:38889` |
| `HTTP_PROXY_AUTH` | `on`    |
| `HTTP_PROXY_PASS` | ``      |

## Release

Push a git tag to build and publish release assets:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow builds linux `amd64` and `arm64` binaries and publishes deb packages that can be started with systemd.

## Credits

- [sobyte](https://www.sobyte.net/post/2021-09/https-proxy-in-golang-in-less-than-100-lines-of-code/)
