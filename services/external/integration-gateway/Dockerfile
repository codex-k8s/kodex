FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY libs/go/ libs/go/
COPY services/external/integration-gateway/ services/external/integration-gateway/
WORKDIR /src/services/external/integration-gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/integration-gateway ./cmd/integration-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/integration-gateway /integration-gateway
ENTRYPOINT ["/integration-gateway"]
