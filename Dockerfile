FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sub2surge .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
USER app
COPY --from=build /out/sub2surge /usr/local/bin/sub2surge
ENV SUB2SURGE_LISTEN=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["sub2surge"]
