# Multi-stage build. The binary has no third-party dependencies and embeds the web UI and catalog.
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shopmoment .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shopmoment /shopmoment
ENV PORT=8080
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/shopmoment"]
