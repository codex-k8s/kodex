FROM golang:1.25.8-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/access-manager ./services/internal/access-manager/cmd/access-manager

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/access-manager /access-manager
EXPOSE 8080 9090
ENTRYPOINT ["/access-manager"]
