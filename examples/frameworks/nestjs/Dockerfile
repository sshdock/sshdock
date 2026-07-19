ARG NEST_CLI_VERSION=11.0.24
ARG NODE_IMAGE=node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd

FROM ${NODE_IMAGE} AS source
ARG NEST_CLI_VERSION
WORKDIR /workspace
RUN npx --yes @nestjs/cli@${NEST_CLI_VERSION} new app --package-manager npm --strict --skip-git

FROM source AS build
WORKDIR /workspace/app
RUN npm run build \
    && npm prune --omit=dev --no-audit --no-fund

FROM build AS runtime-output

FROM ${NODE_IMAGE} AS runtime

ENV NODE_ENV=production
WORKDIR /app

COPY --from=runtime-output --chown=node:node /workspace/app/dist ./dist
COPY --from=runtime-output --chown=node:node /workspace/app/node_modules ./node_modules

USER node
EXPOSE 3000

CMD ["node", "dist/main"]
