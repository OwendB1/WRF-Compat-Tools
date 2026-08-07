FROM scratch

COPY dist/wrf-collector /opt/collector/wrf-collector
COPY dist/agent-manifest.json /opt/collector/agent-manifest.json

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/opt/collector/wrf-collector"]
CMD ["serve"]
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD ["/opt/collector/wrf-collector", "healthcheck"]
