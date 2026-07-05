# Multica self-hosted Nomad job
#
# Prerequisites on each Nomad agent node:
#
#   1. Set node meta in the agent config (client.hcl):
#        meta {
#          home_path          = "/opt/multica"   # base path for bind mounts
#          support_multica_db = "true"            # node that runs postgres
#          support_multica    = "true"            # node(s) that run backend + frontend
#        }
#
#   2. Create data directories on the matching nodes:
#        mkdir -p /opt/multica/pgdata /opt/multica/uploads
#
#   3. Registry must be reachable from the Nomad client node(s).

variable "registry" {
  description = "Docker image registry prefix"
  default     = "docker-registry.imutably.com"
}

variable "image_tag" {
  description = "Tag for multica-backend and multica-web images"
  default     = "codex-khi-159-issue-shell-prototype-e804be63"
}

variable "postgres_db" {
  default = "multica"
}

variable "postgres_user" {
  default = "multica"
}

variable "postgres_password" {
  default = "multica"
}

variable "jwt_secret" {
  default = "change-me-in-production"
}

variable "frontend_origin" {
  description = "Public URL of the frontend (used by backend for CORS / email links)"
  default     = "http://192.168.0.108:13000"
}

variable "multica_app_url" {
  description = "Public URL the app is reachable at"
  default     = "http://192.168.0.108:13000"
}

# ---------------------------------------------------------------------------

job "multica" {
  datacenters = ["dc1"]
  type        = "service"
  node_pool   = "all"

  # ── Postgres ────────────────────────────────────────────────────────────

  group "postgres" {
    count = 1

    constraint {
      attribute = "${meta.support_multica_db}"
      operator  = "="
      value     = "true"
    }

    network {
      port "db" {
        static = 15432
        to     = 5432
      }
    }

    task "postgres" {
      driver = "docker"

      config {
        image = "pgvector/pgvector:pg17"
        ports = ["db"]

        mount {
          type     = "bind"
          target   = "/var/lib/postgresql/data"
          source   = "${meta.home_path}/multica/pgdata"
          readonly = false
        }
      }

      template {
        destination = "local/postgres.env"
        env         = true
        data        = <<EOF
POSTGRES_DB=${var.postgres_db}
POSTGRES_USER=${var.postgres_user}
POSTGRES_PASSWORD=${var.postgres_password}
EOF
      }

      service {
        name     = "multica-postgres"
        port     = "db"
        provider = "nomad"

        check {
          name     = "postgres-tcp"
          type     = "tcp"
          port     = "db"
          interval = "10s"
          timeout  = "5s"
        }
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }

  # ── Backend ─────────────────────────────────────────────────────────────

  group "backend" {
    count = 1

    constraint {
      attribute = "${meta.support_multica}"
      operator  = "="
      value     = "true"
    }

    network {
      port "http" {
        static = 8080
        to     = 8080
      }
    }

    task "backend" {
      driver = "docker"

      config {
        image = "${var.registry}/multica-backend:${var.image_tag}"
        ports = ["http"]

        mount {
          type     = "bind"
          target   = "/app/data/uploads"
          source   = "${meta.home_path}/multica/uploads"
          readonly = false
        }
      }

      # Waits for postgres service registration before rendering DATABASE_URL.
      template {
        destination = "local/postgres.env"
        env         = true
        data        = <<EOF
{{ range nomadService "multica-postgres" -}}
DATABASE_URL=postgres://${var.postgres_user}:${var.postgres_password}@{{ .Address }}:{{ .Port }}/${var.postgres_db}?sslmode=disable
{{- end }}
EOF
        wait {
          min = "3s"
          max = "60s"
        }
      }

      template {
        destination = "local/app.env"
        env         = true
        data        = <<EOF
PORT=8080
JWT_SECRET=${var.jwt_secret}
FRONTEND_ORIGIN=${var.frontend_origin}
APP_ENV=production
ALLOW_SIGNUP=true
MULTICA_APP_URL=${var.multica_app_url}
# Optional — uncomment and fill in to enable email, OAuth, S3, etc.
# RESEND_API_KEY=
# RESEND_FROM_EMAIL=noreply@example.com
# GOOGLE_CLIENT_ID=
# GOOGLE_CLIENT_SECRET=
# GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback
# S3_BUCKET=
# S3_REGION=us-east-1
# AWS_ENDPOINT_URL=
# AWS_ACCESS_KEY_ID=
# AWS_SECRET_ACCESS_KEY=
# ATTACHMENT_DOWNLOAD_MODE=auto
# COOKIE_DOMAIN=
# MULTICA_PUBLIC_URL=
# MULTICA_TRUSTED_PROXIES=
EOF
      }

      service {
        name     = "multica-backend"
        port     = "http"
        provider = "nomad"

        check {
          name     = "backend-http"
          type     = "http"
          path     = "/api/v1/health"
          port     = "http"
          interval = "15s"
          timeout  = "5s"
        }
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }

  # ── Frontend ─────────────────────────────────────────────────────────────

  group "frontend" {
    count = 1

    constraint {
      attribute = "${meta.support_multica}"
      operator  = "="
      value     = "true"
    }

    network {
      port "http" {
        static = 13000
        to     = 3000
      }
    }

    task "frontend" {
      driver = "docker"

      config {
        image = "${var.registry}/multica-web:${var.image_tag}"
        ports = ["http"]
      }

      template {
        destination = "local/app.env"
        env         = true
        data        = <<EOF
HOSTNAME=0.0.0.0
EOF
      }

      service {
        name     = "multica-frontend"
        port     = "http"
        provider = "nomad"

        check {
          name     = "frontend-tcp"
          type     = "tcp"
          port     = "http"
          interval = "15s"
          timeout  = "5s"
        }
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }
}
