FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /agentboard ./cmd/agentboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /agentboard /agentboard
# The database lives on a volume. The nonroot user (65532) must own it.
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/agentboard", "-addr", ":8080", "-db", "/data/board.db"]
