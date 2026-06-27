FROM golang:1.26-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build
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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
