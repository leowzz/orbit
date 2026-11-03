FROM node:24-alpine AS build
WORKDIR /console
RUN npm install --global pnpm@11.9.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . ./
RUN pnpm build

FROM nginx:1.28-alpine
ENV ORBIT_CORE_UPSTREAM=orbit-core:7620
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /console/dist /usr/share/nginx/html
EXPOSE 8080
