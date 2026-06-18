# SCOPE — Example Infrastructure Definition

## Environment

- **Region**: us-west-2
- **VPC**: vpc-prod-01
- **Environment**: production

## Hosts

| Host | Role | OS | Notes |
|------|------|-----|-------|
| web-01 | web server | Ubuntu 22.04 | Nginx, PHP-FPM |
| web-02 | web server | Ubuntu 22.04 | Nginx, PHP-FPM |
| db-01 | database | Ubuntu 22.04 | PostgreSQL 15 |
| cache-01 | cache | Ubuntu 22.04 | Redis 7 |

## Services

- **Web**: Nginx → PHP-FPM on web-* nodes
- **Database**: PostgreSQL 15 on db-01, port 5432
- **Cache**: Redis 7 on cache-01, port 6379

## Constraints

- Sudo requires explicit confirmation
- No direct database modifications without backup
- Deploy only during maintenance window (02:00-04:00 UTC)
- All changes must be idempotent

## Paths

- Configs: `/etc/nginx/`, `/etc/postgresql/15/main/`
- Logs: `/var/log/nginx/`, `/var/log/postgresql/`
- Scripts: `/usr/local/bin/`, `~/bin/`

## Backup Policy

- Database: daily at 01:00 UTC to s3://backups-prod/db/
- Configs: versioned in /etc/backups/
