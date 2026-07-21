FROM alpine:3.22@sha256:7c8cb692ae09657cbc4a3f3cbd0e8d5a2690ba38386aaaf252dbb060bf5eb2e6

COPY index.html /www/index.html

EXPOSE 8080

CMD ["busybox", "httpd", "-f", "-p", "8080", "-h", "/www"]
