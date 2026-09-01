# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/smsc-sim ./cmd/smsc-sim

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/smsc-sim /usr/local/bin/smsc-sim
# SMPP (first operator), metrics, admin. Publish additional operator ports with -p.
EXPOSE 2775 9090 8080
ENTRYPOINT ["/usr/local/bin/smsc-sim"]
CMD ["-config", "/etc/smsc-sim.yaml"]
