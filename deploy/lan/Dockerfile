# Official Go 1.26.6 linux/amd64 image manifest, verified 2026-10-08.
FROM golang:1.26.6-bookworm@sha256:433f9dc4f8ea3a1ce4e28f9f15d0f7c056b10475307f886d6f1ac1ccc4abd976 AS build
ENV GOTOOLCHAIN=local GOFLAGS=-mod=readonly CGO_ENABLED=0
WORKDIR /source/client
COPY client/go.mod client/go.sum ./
COPY client/internal/anet/ ./internal/anet/
COPY client/third_party/gousb/ ./third_party/gousb/
RUN go mod download && go mod verify
COPY client/ ./
RUN GOOS=js GOARCH=wasm go build -trimpath -o web/app.wasm ./cmd/wasm && \
    cp /usr/local/go/lib/wasm/wasm_exec.js web/wasm_exec.js && \
    cp web/index.html web/gui.html && mkdir -p web/models && \
    cp internal/localui/models/*.onnx web/models/ && \
    rm -f web/runtime-policy.test.cjs
WORKDIR /source/host
COPY deploy/lan/server/ ./
RUN go test ./... && go vet ./... && go build -trimpath -o /lan-host .
RUN mkdir -p /out/web && \
    cp /source/client/web/index.html /source/client/web/gui.html /source/client/web/app.wasm /source/client/web/wasm_exec.js /source/client/web/runtime-config.js /source/client/web/runtime-policy.js /source/client/web/bootstrap.js /source/client/web/ai_vision.js /out/web/ && \
    cp -r /source/client/web/vendor /source/client/web/models /out/web/
FROM scratch
COPY --from=build /lan-host /lan-host
COPY --from=build /out/web/ /web/
USER 65532:65532
EXPOSE 8443
ENTRYPOINT ["/lan-host"]
