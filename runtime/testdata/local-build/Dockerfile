FROM alpine:3.22@sha256:7c8cb692ae09657cbc4a3f3cbd0e8d5a2690ba38386aaaf252dbb060bf5eb2e6 AS fetch
ARG HTTP_PROXY
ARG HTTPS_PROXY
RUN wget -qO /downloaded.txt http://example.com/
COPY message.txt /message.txt

FROM scratch
COPY --from=fetch /downloaded.txt /downloaded.txt
COPY --from=fetch /message.txt /message.txt
