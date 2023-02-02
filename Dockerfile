FROM golang:1.20 AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /build
COPY . .
RUN go mod tidy && go build -ldflags "-s -w" -o main


FROM alpine

RUN  apk --update --no-cache add tzdata ca-certificates \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=builder /build/main /

ENTRYPOINT ["/main"]