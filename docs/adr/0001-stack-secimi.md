# ADR 0001 — Stack seçimi: Go + chi + pgx/sqlc + templ/HTMX

- **Durum:** kabul edildi
- **Tarih:** 2026-07-24

## Bağlam

Tappa iki design partner'a (Kebab Factory — 9 lokasyon; Kebab Manufacturing —
5 departman) hizmet verecek, ~100+ çalışan, tek VPS'te AB bölgesinde barınacak.
Kritik gereksinimler: tenant izolasyonu (RLS), NTAG 424 DNA SUN doğrulaması
(AES-CMAC + monoton sayaç), immutable işlem kaydı, düşük operasyon maliyeti
(~€30-50/ay altyapı, ~€200-300 MRR).

Handoff dokümanı backend için "Node (Fastify/Nest) veya Python (FastAPI)"
öneriyordu; mevcut ön yüz taslakları vanilla HTML + React simülasyonuydu.

## Karar

Backend **Go**. Router **chi** (stdlib `net/http` üstüne ince katman),
veri erişimi **pgx/v5 + sqlc** (ORM yok), migration **goose**,
arayüz **templ + HTMX**, CSS **Tailwind standalone CLI**.

Repoda **Node/npm bulunmaz**; Tailwind ikili olarak `.tools/` altına indirilir.

## Gerekçe

- **Tek binary deploy.** Runtime kurulumu, sürüm sürüklenmesi, `node_modules`
  yok. Tek VPS operasyonunu bir kişinin sürdürmesi gerçekçi olur.
- **ORM yok, çünkü kritik SQL'i birebir kontrol etmemiz gerekiyor.** Replay
  koruması tek atomik ifadeye bağlı:
  `UPDATE tags SET last_ctr=$2 WHERE uid=$1 AND last_ctr < $2 RETURNING uid`.
  ORM'lerin oku-değiştir-yaz döngüsü burada sessizce açık yaratır.
- **RLS uygulama rolüne bağlı.** Go tarafında bağlantı yaşam döngüsünü ve
  `SET LOCAL app.tenant_id` yerleşimini açıkça yönetiyoruz; sqlc üretilen kod
  bunu gizlemiyor.
- **Kriptografi stdlib'de.** AES-CMAC için `crypto/aes` + ince bir CMAC
  uygulaması yeterli; üçüncü parti kripto bağımlılığı yok.
- **templ + HTMX**, dashboard için ayrı build hattı, CORS ve ayrı deploy
  yükünü ortadan kaldırır. Tap ekranı zaten tek buton — SPA gereksiz ağırlık.
- **Eşzamanlılık testi birinci sınıf.** `-race` ile replay koruması gerçek
  goroutine yarışına karşı kanıtlanabiliyor.

## Sonuçlar

- Handoff §9'daki mevcut HTML/PWA/React dosyaları **taşınmadı**; landing, portal
  ve tap PWA'sı templ ile yeniden yazılacak. Tasarım dili korunuyor (docket motifi).
- `.templ` ve `db/queries/*.sql` değişince `make gen` zorunlu; üretilen dosyalar
  commit edilir.
- Ekip Go bilmiyorsa öğrenme maliyeti var — kabul edildi, ürünün ömrü boyunca
  operasyon basitliği bunu geri ödüyor.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| FastAPI + SQLAlchemy | Python runtime + bağımlılık yönetimi VPS'te ek yük; ORM kritik SQL'i gizler |
| Fastify/Nest + Prisma | Node ekosistemi; Prisma raw SQL'e düşmeden atomik update zor |
| Fiber + GORM | fasthttp `http.Handler` uyumsuz; GORM ile RLS/atomik update kontrolü zayıflar |
| React SPA dashboard | ikinci build hattı, CORS, ayrı deploy — bu ölçekte karşılığı yok |
