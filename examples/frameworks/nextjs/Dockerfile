ARG CREATE_NEXT_APP_VERSION=16.2.10
ARG NODE_IMAGE=node:24.13.0-slim@sha256:4660b1ca8b28d6d1906fd644abe34b2ed81d15434d26d845ef0aced307cf4b6f

FROM ${NODE_IMAGE} AS source
ARG CREATE_NEXT_APP_VERSION
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
RUN npx --yes create-next-app@${CREATE_NEXT_APP_VERSION} . --yes --use-npm --disable-git

FROM ${NODE_IMAGE} AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=source /app ./
RUN npm run build \
    && npm prune --omit=dev --no-audit --no-fund

FROM build AS runtime-output
RUN rm -rf .next/cache .next/diagnostics .next/types

FROM ${NODE_IMAGE} AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
COPY --from=runtime-output --chown=node:node /app/package.json ./package.json
COPY --from=runtime-output --chown=node:node /app/node_modules ./node_modules
COPY --from=runtime-output --chown=node:node /app/public ./public
COPY --from=runtime-output --chown=node:node /app/.next ./.next
USER node
EXPOSE 3000
CMD ["npm", "start"]
