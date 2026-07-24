# Tappa

**Punchless time & attendance.** NFC tabanlı, cihazsız mesai takip SaaS'ı.

NFC etiketi çalışanın cebinde değil **duvarda**. Her lokasyon girişine pasif bir
plaket (NTAG 424 DNA) monte edilir. Çalışan kendi telefonunu değdirir → tarayıcı
açılır (uygulama kurulumu yok) → kalıcı oturumdan tanınır → tek butonla check-in/out.

> *No app. No device. No fingerprints. Just tap.*

Her tap dört kanıt üretir: **SUN** imzası (şu an fiziksel dokunuş) ·
**oturum çerezi** (kim) · **statik IP** (nerede) · **GPS** (yedek nerede).

## Hızlı başlangıç

Gereksinimler: Go 1.26+, Docker, `make`. **Node gerekmez.**

```bash
cp .env.example .env
make tools          # tailwind ikilisi (tek seferlik)
make up             # postgres
make migrate seed   # şema + demo veri
make dev            # → http://localhost:8080
```

Tüm komutlar: `make help`

## Stack

Go 1.26 · chi · pgx/v5 + sqlc (ORM yok) · goose · PostgreSQL 17 + Row-Level Security ·
templ + HTMX · Tailwind (standalone CLI) · tek binary deploy.

## Dokümantasyon

| Dosya | İçerik |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Çalışma kuralları, mimari sınırlar, **kırmızı çizgiler** |
| [docs/plan/](docs/plan/) | **Çalışma planı** — yol haritası, 81 görev kartı, canlı durum |
| [docs/plan/state.md](docs/plan/state.md) | **Nerede kaldık** — her oturum buradan başlar |
| [docs/handoff.md](docs/handoff.md) | Ürün: problem, pazar, marka, müşteriler, yol haritası |
| [docs/adr/](docs/adr/) | Mimari kararlar ve gerekçeleri |

## Lisans

Özel — tüm hakları saklıdır.
