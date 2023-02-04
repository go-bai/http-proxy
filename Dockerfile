FROM golang:1.20 AS builder
RUN apk --no-cache add tzdata
WORKDIR /build
COPY . .
RUN go mod tidy && go build -ldflags "-s -w" -o main


FROM scratch
COPY --from=builder /user/share/zoneinfo /user/share/zoneinfo
COPY --from=builder /build/conf conf
COPY --from=builder /build/main /
ENV TZ=Asia/Shanghai

ENTRYPOINT ["/main"]