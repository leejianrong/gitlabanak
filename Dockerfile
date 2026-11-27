FROM --platform=$BUILDPLATFORM golang:1.26.11 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/gitlabanak ./cmd/gitlabanak
RUN mkdir /out/data

FROM scratch
COPY --from=build /out/gitlabanak /gitlabanak
# An empty /data owned by the runtime user, so a new volume mounted there is writable.
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/gitlabanak"]
