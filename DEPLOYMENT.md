# Deploying Vintage Social Counter

This repo is now set up for a practical first public deployment:

- a DigitalOcean Droplet runs Docker Compose
- GoDaddy keeps the domain registration
- your public domain points to the Droplet
- Caddy handles HTTPS automatically

## Recommended stack

For the current codebase, the best fit is a single Ubuntu Droplet.

Reason:

- the app stores creator sessions and published boards on local disk
- a single Droplet with persistent storage is simpler than a more stateless platform
- the public board is read-only, so one server is enough for the first real version

## What "public" means here

- Creators use `/dashboard` as the private dashboard.
- Viewers use `/board/<slug>` as the read-only public wallboard.
- Public viewers do not need to connect YouTube or Instagram.
- If you set `PUBLIC_ROOT_BOARD_SLUG`, the root URL `/` can redirect straight to a published board so your main domain becomes the public counter page.
- If you do not set it and there is exactly one published board, the app will use that board automatically for `/`.

## 1. Create a Droplet

Create an Ubuntu Droplet in DigitalOcean and SSH into it.

Recommended for the first launch:

- Ubuntu LTS
- basic shared CPU
- at least 1 GB RAM
- backups enabled if this matters to you

## 2. Install Docker on the Droplet

On the server, install Docker and Docker Compose support, then clone this repo.

Example project location:

```bash
git clone <your-repo-url> vintage-social-counter
cd vintage-social-counter
```

## 3. Decide where DNS will live

You have 2 valid choices:

- keep DNS in GoDaddy and add `A` records to the Droplet IP
- switch the domain’s nameservers to DigitalOcean and manage DNS there

For this project, I recommend moving DNS to DigitalOcean because it keeps the server and DNS in one place.

If you do that, update your GoDaddy nameservers to:

- `ns1.digitalocean.com`
- `ns2.digitalocean.com`
- `ns3.digitalocean.com`

Then create DNS records in DigitalOcean for:

- `counter.yourdomain.com` -> your Droplet IPv4

If you want to keep DNS in GoDaddy instead, create an `A` record there pointing your hostname to the Droplet IP.

## 4. Create your production `.env`

Copy [.env.example](/Users/wenxuan/Desktop/youtube-stats/.env.example:1) to `.env` and fill in the real values.

Important values:

- `APP_DOMAIN=counter.yourdomain.com`
- `PUBLIC_BASE_URL=https://counter.yourdomain.com`
- optional: `PUBLIC_ROOT_BOARD_SLUG=board-abc123xy`
- `GOOGLE_CLIENT_ID=...`
- `GOOGLE_CLIENT_SECRET=...`
- `YOUTUBE_REDIRECT_URL=https://counter.yourdomain.com/auth/youtube/callback`
- `INSTAGRAM_CLIENT_ID=...`
- `INSTAGRAM_CLIENT_SECRET=...`
- `INSTAGRAM_REDIRECT_URL=https://counter.yourdomain.com/auth/instagram/callback`
- `TIKTOK_CLIENT_KEY=...`
- `TIKTOK_CLIENT_SECRET=...`
- `TIKTOK_REDIRECT_URL=https://counter.yourdomain.com/auth/tiktok/callback`
- `SESSION_ENCRYPTION_KEY=...`

Generate the encryption key once and keep reusing the same value for future deploys:

```bash
openssl rand -base64 32
```

## 5. Update OAuth callback URLs

Replace the local callback URLs in Google Cloud and Meta with your real domain:

- YouTube:
  - `https://counter.yourdomain.com/auth/youtube/callback`
- Instagram:
  - `https://counter.yourdomain.com/auth/instagram/callback`
- TikTok:
  - `https://counter.yourdomain.com/auth/tiktok/callback`

These must exactly match your `.env` values.

## 6. Start the stack

For public deployment, run the `public` profile so Caddy starts too:

```bash
docker compose --profile public up --build -d
```

What starts:

- `app` on internal port `8080`
- `caddy` on ports `80` and `443`

The reverse proxy config is in [Caddyfile](/Users/wenxuan/Desktop/youtube-stats/Caddyfile:1).

## 7. Check it

Health check:

```bash
curl https://counter.yourdomain.com/healthz
```

Expected response:

```json
{"status":"ok"}
```

Then test the product flow:

1. Open `https://counter.yourdomain.com/`
2. If you configured `PUBLIC_ROOT_BOARD_SLUG`, open `https://counter.yourdomain.com/dashboard` for the creator dashboard instead.
3. Connect YouTube and/or Instagram in the creator dashboard
4. Choose platforms and metrics
5. Click `Publish Link`
6. Open the generated `/board/<slug>` link in another browser or device

## Local vs public

For local-only testing:

```bash
docker compose up --build -d
```

For the real public domain:

```bash
docker compose --profile public up --build -d
```

## Current production shape

This is good for:

- one always-on server
- one containerized app
- one public hostname
- persistent local disk in `./data`

This is not yet the final architecture for:

- multiple app servers behind a load balancer
- creator login across multiple devices
- shared database-backed accounts

That next step would be moving session and board storage out of local JSON files and into a shared database.
