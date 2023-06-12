## Dockerfile for release, as lightweight as possible

ARG ARCH
ARG GO_VERSION
ARG GOARCH

FROM --platform=linux/amd64 golang:${GO_VERSION} AS builder

WORKDIR /go/src/sigs.k8s.io/windows-gmsa/admission-webhook

# copy go dependencies
COPY go.mod ./go.mod
COPY go.sum ./go.sum

# build
COPY *.go ./
ARG VERSION
RUN go mod vendor && go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} go build -ldflags="-w -s -X main.version=${VERSION}"

###

FROM --platform=linux/${ARCH} scratch

WORKDIR /admission-webhook

ENV LOG_LEVEL=info

COPY --from=builder /go/src/sigs.k8s.io/windows-gmsa/admission-webhook/admission-webhook .

ENTRYPOINT ["/admission-webhook/admission-webhook"]
