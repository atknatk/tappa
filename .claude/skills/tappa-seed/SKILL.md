---
name: tappa-seed
description: Tappa'nın demo/test verisi — Kebab Factory (9 lokasyon) ve Kebab Manufacturing (5 departman) tenant'ları, çalışanlar, plaketler, vardiyalar ve "bir günü simüle et" işlem üreteci. Seed yazarken, fixture eklerken, dashboard'a örnek veri lazım olduğunda, demo/ekran görüntüsü hazırlarken veya entegrasyon testi kurarken oku.
---

# Tappa demo verisi

Ekran ve testler **gerçek** design partner verisiyle çalışır. Uydurma isim/şirket
üretme — bu iki tenant satış demolarında da kullanılıyor, tutarlılık önemli.

## Tenant 1 — Kebab Factory Ltd. (multi-location)

`structure: multi` · 9 lokasyon · her lokasyonun **kendi statik dış IP'si** var.

| Lokasyon | Vardiya | Not |
|---|---|---|
| KF Hamrun | 10:00–22:00 | restoran |
| KF Mellieha | 10:00–22:00 | restoran |
| KF Msida | 10:00–22:00 | restoran |
| KF Valletta | 10:00–22:00 | restoran |
| KF San Gwann | 10:00–22:00 | restoran |
| KF St Julians | 10:00–22:00 | **referans senaryo** — 15 kişilik kadro, günde ~10 kişi |
| KF Paceville | 11:00–23:00 | |
| Rusty Bar | **18:00–02:00** | `overnight = true` — çıkış ertesi güne sarkar |
| KF Headquarter | 09:00–17:00 | ofis |

Departman yok (lokasyon = operasyon birimi).

## Tenant 2 — Kebab Manufacturing Co. Ltd. (single-location)

`structure: single` · tek tesis = **tek dış IP** · 5 departman.
IP yalnızca "tesiste" kanıtıdır; departman çalışanın profilinden çözülür.

| Departman | Vardiya |
|---|---|
| KM General | 09:00–17:00 |
| KM Meat Production | 05:00–13:00 |
| KM Warehouse & Supply | 08:00–16:00 |
| KM Central Kitchen | 06:00–14:00 |
| KM Pastry & Bakery | 04:00–12:00 |

**Geç kalma kişinin KENDİ vardiyasına göre hesaplanır** — departman vardiyası
varsa o, yoksa lokasyon vardiyası. Seed bu ayrımı test edecek veri içermeli.

Hiyerarşi: `Tenant → Location (statik IP + GPS) → Department (ops.) → Employee`

## Sabit değerler

- **Zaman dilimi:** Malta (UTC+1 / yazın UTC+2). DB'ye **UTC** yazılır; seed
  saatleri Malta yerel saatinden çevrilir. Bu çeviriyi seed'de açıkça yap ve
  yorumla — gece vardiyası hatalarının çoğu burada doğar.
- **GPS:** Malta koordinatları (~35.9 N, 14.5 E). Lokasyonlar birbirinden en az
  birkaç yüz metre uzak olsun ki 150 m yarıçap testi anlamlı olsun.
- **Statik IP'ler:** dokümantasyon aralıkları kullan (`203.0.113.0/24`,
  `198.51.100.0/24`). Gerçek müşteri IP'sini repoya yazma.
- **Tag UID:** 14 hane hex, okunabilir olsun (`91AC7E5500000A` gibi).
  Seed'de **sahte** AES anahtarları — gerçek etiket anahtarı asla repoda.
- **İsimler:** Malta'da yaygın isimler (Maria Borg, Joseph Camilleri,
  Antoine Vella, Rita Zammit…). Kadro çok uluslu — bir kısmı da farklı kökenli olsun.

## "Bir günü simüle et" — KF St Julians

Karar motorunu uçtan uca sınamak için tek komutla üretilen bir gün:
**10 çalışan, ~21 işlem**. Şunları **mutlaka** içermeli:
*(Bu tablo bir **spec**'tir. Bugün fiilen üretilen gün ile arasındaki dört ölçülmüş
fark — sayı, geç kalma satırı, kadronun kim olduğu ve bir motor bulgusu — artı
günün gerçek zaman maliyeti, hemen tablonun altındaki ölçüm notunda.)*

🔴 **Bu cümle eskiden "dashboard'u gerçekçi göstermek ve…" diye başlıyordu; o yarısı
BUGÜN KARŞILANMIYOR ve ölçüme eşitlendi.** Gün, seed'li kadroyu değil, `full_name`'i
**koşum damgalı** taze çalışanları sürüyor (`Maria Borg [sim 08-02T00:40:24 f4ef]`)
— ve damga **kullanıcıya görünen yüzeye sızıyor**: `employees.full_name` →
`TapPageFacts.EmployeeName` (`internal/domain/tenant/directory.go`) → tap ve
aktivasyon ekranlarındaki "Hello …"; M6 dashboard'u da aynı sütunu okuyacak. Yani
bugünkü çıktı **ekran görüntüsüne hazır değil**. Sebep, ölçüm ve değerlendirilen
alternatifler: aşağıdaki not + M5-09 kart düzeltmesi md. 6 eki.

| Senaryo | Beklenen kayıt |
|---|---|
| Normal giriş + çıkış (çoğunluk) | `ok`, trust 100, ip ✓ gps ✓ |
| Geç kalma (vardiya 10:00, giriş 10:17) | `ok` + rapor "late 17m" |
| Çift tap — aynı kişi 20 sn içinde | `ignored` (debounce **kişi** bazlı) |
| Ardışık farklı kişiler, aynı plaket, 10 sn arayla | **hepsi** `ok` — debounce tetiklenmemeli |
| Mobil veriyle giriş (IP eşleşmiyor, GPS ✓) | `ok`, trust 50, not `verified via GPS` |
| IP yok + GPS reddedildi | `flag` — onay kuyruğuna düşer |
| Deaktive edilmiş çalışanın denemesi | `reject` + güvenlik uyarısı |
| Retired plakete tap | `reject` |
| QR ile giriş (SUN yok, IP ✓) | `ok`, `channel='qr'` |
| Yeni çalışanın practice tap'i | `practice=true`, TRAINING, saate sayılmaz |
| Telefonsuz çalışan — müdür manuel girer | `channel='manual'`, `entered_by` dolu |
| Rusty Bar gece vardiyası: 18:05 giriş, ertesi gün 02:10 çıkış | çıkış doğru girişle eşleşir |

Son satır kritik: **yön tayini son açık girişe göre**, takvim gününe göre değil.
Bu senaryo olmadan gece vardiyası regresyonu fark edilmez.

> **Ölçüm notu (2026-08-01, 2. turda genişletildi 2026-08-02 — M5-09).** Gün artık
> üretiliyor: `make simulate-day` (kayıtları karar motoru yazar — gerçek HTTP,
> gerçek router, gerçek Postgres; `internal/handler/day_db_test.go`). Yukarıdaki
> tablo **spec olarak duruyor**; bugün fiilen üretilenle arasındaki farklar
> ölçüldü ve şunlar:
> - **"~21 işlem" değil, 31.** §5 *"aktivasyon sonrası ilk kayıt practice"* diyor;
>   taze seed'de kimsenin kaydı olmadığı için tap eden **her** çalışan bir TRAINING
>   satırıyla başlıyor. Sayı senaryoların toplamıdır, hedef değil.
> - 🔴 **"Geç kalma (vardiya 10:00, giriş 10:17) → `ok` + rapor 'late 17m'" satırı
>   BUGÜN ÜRETİLMİYOR.** `MinutesLate` hiçbir sütuna/`policy_context`'e/ekrana
>   yazılmıyor → HTTP'den **gözlemlenemez**; hesap ayrıca sunucu saatinden yapılıyor
>   (kaydın `occurred_at`'inden değil) → geriye tarihli girişte **yanlış**. Tek
>   gözlem noktası, çağıranın `Decision`'ı elinde tuttuğu manuel giriştir. 1. turda
>   bunu örten "vardiya başlangıcından sonra" assertion'ı **yapısal olarak boştu**
>   (mutasyonla ölçüldü) ve kaldırıldı. Ayrıntı: M5-09 kart düzeltmesi md. 2.
> - **Gün ~62 sn gerçek zamanda bekliyor.** ADR 0006 debounce'u sunucu saatiyle
>   ölçtüğü için aynı kişinin ardışık tap'leri sıkıştırılamıyor (beklemesiz: 15
>   kaydın 10'u `ignored`).
> - 🔴 **Bu gün DASHBOARD'U SEED'Lİ KADROYLA DOLDURMUYOR** — cümlenin ölçüme
>   eşitlenmiş hâli budur. Simülasyon, seed'li isimleri taşıyan ama **taze** ve
>   `full_name`'i **koşum damgalı** çalışanlar yaratıyor
>   (`Maria Borg [sim 08-02T00:40:24 f4ef]`); seed'li asıl Maria Borg'un işlemi
>   olmuyor. Sebep ölçüldü: `transactions` değişmez (§4.3) ve temizlik yok, yani
>   sabit bir kadro bu günü **veritabanı ömrü başına bir kez** ağırlayabilir
>   (2. koşuda aktivasyon turu atlanıyor, practice tap'i kalmıyor, önceki koşudan
>   açık giriş devralınıyor). Damga sayesinde ana veri ile simülasyon çıktısı
>   **ad üzerinden ayrılabiliyor** — M6 dashboard'u bunu bilmeli. Ayrıntı:
>   M5-09 kart düzeltmesi md. 6 eki, `day_db_test.go` LIMITS L4.
> - 🔴 **Motorda açık bir bulgu var ve gün onun etrafından dolaşıyor:** daha yeni
>   `occurred_at`'li bir practice satırı, altındaki gerçek açık girişi maskeliyor
>   → çıkış `in` oluyor. Ivan'ın practice tap'i bu yüzden 17:50 **beyan ediliyor**.
>   Fixture'ı kopyalarken bu satırı da kopyalama, **sebebini** oku:
>   M5-09 kart düzeltmesi md. 6.

## Nasıl yazılır

- `test/fixtures/seed.sql` — idempotent (sabit UUID'ler + `ON CONFLICT DO NOTHING`),
  `make seed` ile çalışır.
- ⚠️ **`make seed` İKİ ADIMDIR ve `TAPPA_TAG_KEK` ZORUNLUDUR** — yoksa
  `scripts/seed.sh` exit 1 verir, yarım seed bırakmaz. Adım 1 `seed.sql`'i akıtır;
  adım 2 `test/fixtures/seedkeys` ile her demo plaketin per-tag anahtarını KEK ile
  **sarmalar** ve `tags.aes_key_ref`'i yazar. Bu SQL'de yapılamaz: pgcrypto'da GCM
  yok ve değer operatörün KEK'ine bağlı (KEK repoda değil — §4.7). Anahtarın hiç
  yoksa üret: `openssl rand -base64 32` → `.env`.
- ⚠️ **Yeni plaket = İKİ yer:** `seed.sql`'e satır **ve** `fixtures.SeedTags`'e
  giriş (`test/fixtures/tagkeys.go`). Yalnız birine yazarsan plaket 44 baytlık
  zarfsız kalır ve ilk tap `GET /t`'de **500** verir. Drift guard bunu mekanik
  yakalar: seed adımı 44 bayt olmayan bir demo plaket görürse `RAISE` ile patlar,
  yani hata günler sonra ilk tap'te değil, `make seed`'de çıkar.
- Tarihler `now()`'a **göreli** üretilir (bugünün vardiyası) ki dashboard her
  zaman dolu görünsün; sabit takvim tarihi gömme.
- Üretilen `transactions` satırları karar motorunun **çıktısıyla tutarlı** olmalı:
  `verdict`, `trust`, `ip_match`, `gps_match`, `sun_valid` elle uydurulmaz —
  §5'teki tabloya göre hesaplanır. Tutarsız seed, hatalı motoru doğru gösterir.
- İki tenant arasında **hiçbir** paylaşılan satır olmasın; seed aynı zamanda
  RLS izolasyon testinin zeminidir.
- Entegrasyon testleri seed'e bağımlıysa sabit UUID'leri Go tarafında adlandırılmış
  sabit olarak tut (`test/fixtures/ids.go`), sorguda sihirli string kullanma.

Ürün bağlamının tamamı: [docs/handoff.md](../../../docs/handoff.md) §5.
