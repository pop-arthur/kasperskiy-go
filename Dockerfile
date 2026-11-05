FROM golang:1.27.3-alpine AS build
WORKDIR /src
ENV GOTOOLCHAIN=local
COPY go.mod ./
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api

FROM scratch
COPY --from=build /out/api /api
USER 65532:65532
ENTRYPOINT ["/api"]
