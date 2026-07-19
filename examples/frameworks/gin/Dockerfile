ARG GIN_EXAMPLES_REVISION=70ea0357aca8fab6638a85709ff74d51c1bb0e73
ARG GO_IMAGE=golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc
ARG ALPINE_IMAGE=alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

FROM ${GO_IMAGE} AS source
ARG GIN_EXAMPLES_REVISION
RUN apk add --no-cache git
WORKDIR /workspace
RUN git clone https://github.com/gin-gonic/examples.git . \
    && git checkout --detach ${GIN_EXAMPLES_REVISION} \
    && test "$(git rev-parse HEAD)" = "${GIN_EXAMPLES_REVISION}"

FROM source AS build
RUN mkdir -p /out \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gin-basic ./basic

FROM ${ALPINE_IMAGE} AS runtime
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/gin-basic ./gin-basic
USER 65532:65532
EXPOSE 8080
CMD ["./gin-basic"]
