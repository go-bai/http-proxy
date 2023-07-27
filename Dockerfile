FROM golang:1.20-alpine AS builder
RUN apk --no-cache add tzdata
RUN apk add upx
ENV GO111MODULE=on \
    CGO_ENABLED=0
WORKDIR /build
COPY . .
RUN go mod tidy && go build -ldflags "-s -w" -o main && upx -9 main


FROM scratch
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=builder /build/main /

ENTRYPOINT ["/main"]