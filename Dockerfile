FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY service ./service
COPY sdk ./sdk
COPY conf ./conf
RUN CGO_ENABLED=0 go build -tags=nomsgpack -trimpath -ldflags='-s -w' -o /forge ./cmd/server \
    && CGO_ENABLED=0 go build -tags=nomsgpack -trimpath -ldflags='-s -w' -o /forge-init ./cmd/init
FROM scratch
COPY --from=build /forge /forge
COPY --from=build /forge-init /forge-init
COPY --from=build /src/conf /conf
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENV GIN_MODE=release
EXPOSE 8080
ENTRYPOINT ["/forge"]
