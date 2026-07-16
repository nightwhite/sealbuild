FROM alpine:3.22 AS build

RUN apk add --no-cache ca-certificates
COPY input.txt /work/input.txt
RUN test "$(cat /work/input.txt)" = "sealbuild-runtime-spike"

FROM scratch
COPY --from=build /etc/alpine-release /alpine-release
COPY --from=build /work/input.txt /input.txt
