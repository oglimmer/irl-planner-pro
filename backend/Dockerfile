FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION="dev"
ARG GIT_COMMIT="unknown"
ARG BUILD_TIME="unknown"
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X irlplanner/internal/buildinfo.Version=${VERSION} \
      -X irlplanner/internal/buildinfo.Commit=${GIT_COMMIT} \
      -X irlplanner/internal/buildinfo.Time=${BUILD_TIME}" \
    -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
