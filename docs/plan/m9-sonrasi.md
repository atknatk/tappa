# M9 — Pilot sonrası

**Amaç.** Handoff §10.7'deki "sonrası" maddeleri. Bunlar MVP'nin parçası
**değildir**; pilot verisi hangisinin gerçekten gerektiğini gösterecek.

Bu milestone'un görevleri bilinçli olarak daha az ayrıntılıdır. Biri sıraya
alındığında kartı önce detaylandırılır, sonra başlanır.

---

## M9-01 — Çevrimdışı kuyruk

- **Bağımlılık:** M5-09
- **Commit:** `feat(pwa): queue failed taps offline`

**Amaç.** Başarısız POST'u lokalde kuyruğa alıp online olunca yeniden denemek.

**Notlar.**
- Sunucu tarafı hazırlığı M5-05'te yapıldı: `occurred_at` istemciden,
  `created_at` sunucudan; imzalı `ctr` geç senkronda da geçerli.
- Kuyruktan gelen kayıt "queued" olarak işaretlenir ve raporda görünür.
- Service worker + manifest gerekir; **Node yok** — el yazımı `sw.js`.
- Kuyruk telefonun deposunda tutulur; oturum token'ı kuyruğa yazılmaz.

---

## M9-02 — Yönetici push bildirimleri

- **Bağımlılık:** M6-04
- **Commit:** `feat(notify): add vapid web push for managers`

**Notlar.** VAPID anahtarları config'te (`TAPPA_VAPID_*`, `.env.example`'da
yer tutucu var). Bildirim tetikleyicileri: FLAGGED kayıt, deaktive hesap
denemesi, plaket sessizliği (bir lokasyondan gün boyu tap gelmemesi).

---

## M9-03 — BioTime CSV içe aktarma

- **Bağımlılık:** M6-05
- **Commit:** `feat(import): import employees from biotime csv`

**Notlar.** Onboarding'i saatlerden dakikalara indirir; pilot sonrası ikinci
firmaya yayılımda doğrudan işe yarar. Eşleme önizlemesi + kuru koşum zorunlu;
içe aktarma `audit_log`'a yazılır. **Biyometrik alan varsa içe alınmaz** (§4.1).

---

## M9-04 — Tenant marka mesajı editörü

- **Bağımlılık:** M7-05
- **Commit:** `feat(dashboard): add brand message editor`

**Notlar.** Onay ekranındaki mesajlar panelden düzenlenebilir olur (handoff §4).
Metin uzunluğu sınırlı, HTML yok (XSS), emoji serbest. Varsayılana dönüş butonu.

---

## M9-05 — Çalışan self-service saat görünümü

- **Bağımlılık:** M6-07
- **Commit:** `feat(portal): let employees view their own hours`

**Notlar.** ⚠️ Bu, tap ekranına **özellik eklemek değildir** — ayrı bir sayfa
olmalı. Tap ekranı tek ekran/tek buton olarak kalır (CLAUDE.md §9). Çalışan
yalnızca kendi kayıtlarını görür; RLS'e ek olarak `employee_id` filtresi.

---

## M9-06 — Policy simülatörü

- **Bağımlılık:** M6-09 · M3-04 · M3-07
- **Commit:** `feat(dashboard): simulate policy changes against past taps`

**Amaç.** "Bu kuralı açarsam ne değişir?" sorusunu **kayıt değiştirmeden**
cevaplamak. AWS policy simulator'ın karşılığı.

**Neden ertelendi (Q22).** M3 v1 kapsamı daraltıldı: pilot öncesi tenant zaten
serbest belge yazmıyor, yalnız baseline'ı form üzerinden açıp kapatıyor.
Simülatörün asıl değeri, müşteri kendi kurallarını yazmaya başladığında ortaya
çıkar.

**Kabul kriterleri.**
- Seçilen tarih aralığındaki gerçek tap'ler **yeniden değerlendiriliyor**
  (kayıtlara yazılmadan — `transactions` immutable, §4.3).
- **`transactions.policy_context` anlık görüntüsünden** okunuyor, bugünün
  vardiya/profil verisinden değil. Bu sütun M3-07'de **1. günden** yazılıyor;
  olmadan simülasyon yanlış sonuç verir (vardiya tanımı veya çalışanın profili
  sonradan değişmiş olabilir).
- Çıktı: "geçen hafta 214 tap → 6 kayıt `ok`'tan `flag`'e geçerdi" + etkilenen
  docket listesi.
- Simülasyon deterministik (M3-04'teki sıralama garantisi buna dayanıyor).
- **Guardrail'ler simülasyonda da uygulanıyor** — müşteri "kapatırsam ne olur"u
  deneyemiyor bile.
- Tarih aralığı sınırlı ve iş arka planda/kuyrukta koşuyor: tek VPS'te tek
  process var (CLAUDE.md §1); sınırsız aralık tüm tenant'lar için servisi durdurur.

---

## M9-07 — Ham JSON politika editörü

- **Bağımlılık:** M6-09 · M3-03
- **Commit:** `feat(dashboard): add raw policy document editor`

**Amaç.** İleri kullanıcının sıfırdan politika belgesi yazabilmesi.

**Neden ertelendi (Q22).** v1'de tenant baseline'ı form üzerinden ayarlıyor.
Serbest belge yazımı, motorun tüm yüzeyini (10 operatör, 21 bağlam anahtarı,
kaynak eşleme) müşteriye açar — pilot öncesi gereksiz risk ve iş.

**Kabul kriterleri.**
- Canlı doğrulama (M3-03): bilinmeyen effect/action/operatör/anahtar anında hata.
- `ignore`/`redirect` ve `sys:` ad alanı editörde de reddediliyor.
- Nicel sınırlar (belge boyutu, ifade sayısı, kota) editörde gösteriliyor.
- Kaydetmeden önce **M9-06 simülasyonu** zorunlu adım.
- Sürüm geçmişi ve geri alma; düzenleme yeni sürüm üretiyor.
