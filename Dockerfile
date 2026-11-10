# The Apify actor "Triremes website-gap leads". main.go uses the
# standard library only, so the build needs nothing but this repository.
FROM golang:1.27.1 AS build
WORKDIR /actor
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /triremes-apify .

FROM gcr.io/distroless/static-debian12
# When it was built: docker build --build-arg CREATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ);
# Apify's own builds leave it unset, and its build log has the time.
ARG CREATED_AT=unset
LABEL org.pigfox.estate="TR" \
      org.pigfox.created-by="pigfox/triremes-apify-actor Dockerfile" \
      org.pigfox.created-at="${CREATED_AT}" \
      org.pigfox.purpose="Apify actor: Triremes website-gap leads through API v1"
COPY --from=build /triremes-apify /triremes-apify
ENTRYPOINT ["/triremes-apify"]
