FROM golang:1.20-alpine AS builder
RUN apk --no-cache add tzdata
ENV GO111MODULE=on \
    CGO_ENABLED=0
WORKDIR /build
COPY . .
RUN go mod tidy && go build -ldflags "-s -w" -o main


FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /build/conf conf
COPY --from=builder /build/main /
ENV TZ=Asia/Shanghai

ENTRYPOINT ["/main"]