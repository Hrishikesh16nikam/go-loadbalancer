FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/lb ./cmd/lb \
 && CGO_ENABLED=0 go build -o /out/backend ./examples/backend

FROM alpine:3.20
COPY --from=build /out/ /usr/local/bin/
COPY configs/ /etc/lb/
EXPOSE 8080 9090
ENTRYPOINT ["lb"]
CMD ["-config", "/etc/lb/docker.json"]
