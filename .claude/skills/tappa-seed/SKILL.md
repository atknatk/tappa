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

Dashboard'u gerçekçi göstermek ve karar motorunu uçtan uca sınamak için tek
komutla üretilen bir gün: **10 çalışan, ~21 işlem**. Şunları **mutlaka** içermeli:

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

## Nasıl yazılır

- `test/fixtures/seed.sql` — idempotent (sabit UUID'ler + `ON CONFLICT DO NOTHING`),
  `make seed` ile çalışır.
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
