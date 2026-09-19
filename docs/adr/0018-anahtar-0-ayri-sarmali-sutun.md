# ADR 0018 — Anahtar 0 (AppMasterKey) plaket-başına ayrı bir sarmalı sütunda durur

- **Durum:** kabul edildi
- **Tarih:** 2026-09-20
- **Bağlam:** [M8-05](../plan/m8-deploy-pilot.md) encode hattı — [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)
  §6 **madde 5**'i (state.md kısaltmasıyla *"md. 5"*) karara bağlar. 0017 §5.1
  **step 8** (`ChangeKey` uygulama anahtarı 0) *normatif ama bir şema kararına
  bağlı* diye sevk edilmemişti; bu ADR o şemayı seçer ve step 8'in önünü açar.
- **İlgili:** [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) (§5.0
  Karar 1 — iki yetki tek sırra kenetlenmez · §5.1 adım sırası · §5.2/§5.3
  yarım-yazma ve kurtarma · §6 md. 5 — **bununla KAPANIR**) ·
  [ADR 0003](0003-sdm-modu-ve-anahtar-yonetimi.md) md. 3 (park geneli tek master
  yasağı) · CLAUDE.md §4.6, §4.7, §6 · migration
  [00023](../../db/migrations/00023_add_app_key_ref_to_tags.sql)

## Neden ayrı bir ADR

ADR 0017 §6, *"ne KARAR VERİLMEDİ"*'yi dürüstçe sayan bir listedir; madde 5 orada
**üç saklama şıkkı sayılıp hiçbiri seçilmeden** bırakıldı ve step 8'in ön koşulu
ilan edildi. Bir §4.7 anahtar sınırı ve kalıcı bir şema (migration) kararı sessizce
verilemez; bu yüzden istisna değil, seçim gerekçesiyle ve **elenen iki şıkkın
fiyatıyla** yazılıyor.

## Bağlam

Bugün encode hattı (10 exchange, `internal/encode/driver.go`) yalnız **anahtar 1**'i
(`K_SDMFileRead`) kişiselleştiriyor: plaket-başına rastgele AES-128 (`mintPlaqueKey`,
`crypto/rand`), `TAPPA_TAG_KEK` ile AES-GCM sarmalanıp `tags.aes_key_ref`'e (44 bayt)
yazılıyor (Q06 kararı). **Anahtar 0 (AppMasterKey) hâlâ halka açık fabrika
varsayılanında** (sıfırlar); auth bu değerle yapılıyor. 0017 §5.0'ın adıyla:
fiziksel erişimi olan herkes master olarak `AuthenticateEV2First` yapıp `WriteData`
ile NDEF URL host'unu değiştirebilir (**oltalama**, ADR 0005 risk 8), `ChangeFileSettings`
ile SDM'i kapatabilir ya da `ChangeKey` ile çipi kilitleyebilir. Bu yüzden
**anahtar 0 fabrika varsayılanındayken bir plaket DUVARA ÇIKAMAZ** (0017 §5.1).

`internal/sun/changekey.go` komut katmanı **zaten hazır**: case 2 (anahtar 0) 17
baytlık `NewKey ‖ KeyVer` gövdesini (XOR yok, CRC yok) üretir ve bu değişimin
**oturumu bitirdiğini** bilir. Eksik olan tek şey, çipe yazılacak yeni anahtar 0'ın
**nerede saklanacağı** — `internal/encode/session.go`'daki `K_AppMaster` slotu
*tanımlı ama hiç doldurulmuyor*, ve onu tutacak bir sütun yok.

## Karar

Anahtar 0, anahtar 1 ile **aynı hayat**ı yaşar ama **ayrı bir alanda** durur:

1. **Plaket-başına rastgele**, `crypto/rand`'dan bağımsız bir AES-128 (anahtar 1'in
   kopyası ya da türevi DEĞİL — ADR 0003 md. 3).
2. `TAPPA_TAG_KEK` ile AES-GCM **sarmalanır** (aynı `KEKWrapper`, aynı 44-bayt zarf).
3. **Yeni bir sütunda** saklanır: `tags.app_key_ref bytea` — `aes_key_ref`'ten
   ayrı, **nullable** (00023-öncesi satırlar onsuz doğdu; yeni akışta satır
   yaratılırken dolar), **yaz-bir-kez** (00013'ün `aes_key_ref` triggerinin
   ikizi), düz anahtar **asla**.
4. Encode akışında yeni anahtar 0 üretilip sarmalanıp `app_key_ref`'e **çipe
   dokunmadan önce** yazılır (0017 §5.2 asimetrisi: *"çip var, satır yok"* kalıcı
   kayıptır → DB önce, çip sonra). Çipe `ChangeKey(0x00)` **step 8'de, en son**
   gönderilir (oturumu bitirdiği için — 0017 §5.1).

## Gerekçe — ve elenen iki şıkkın fiyatı

Üç şık sayıldı; ikisi **ölçülerek** elendi.

- **Şık 2 — plaket sırrından KDF ile türetme** (`key0 = KDF(plaketAnahtarı, …)`,
  sütunsuz, 44 bayt sabit): **ELENDİ.** İki ayrı yetkiyi — SDM okuma (tap
  doğrulaması) ve master (çipin tüm kontrolü) — **tek bir sırra kenetler**.
  `K_SDMFileRead` sızarsa master da türetilebilir. Bu tam olarak 0017 §5.0
  **Karar 1**'in reddettiği kenetlemedir; iki yetki iki ayrı güvenlik sınırıdır.
  (ADR 0003 md. 3'ü bozmaz — türetilen sır yine plaket-başınadır — ama izolasyonu
  bozar, ki asıl mesele o.)
- **Şık 3 — yaz-ve-at** (rastgele yaz, değeri saklama): **ELENDİ.** Çip step 8
  sırasında yarı-yazılırsa (güç kesintisi) anahtar 0 değişir ama **hiçbir yerde
  bulunmaz** → çip kalıcı olarak çöptür ve 0017 §5.3'ün yarım-yazma sondaları onu
  **teşhis edemez**. Bu, 0017 §5.2'nin *"çip var, satır yok"* kalıcı-kayıp modu ve
  CLAUDE.md §4.6 (kayıt asla kaybolmaz) ruhuyla çelişir.
- **Şık 1 — ayrı sütun + migration:** **SEÇİLDİ.** İki anahtar iki ayrı sarmalı
  alanda → **izolasyon** (biri sızsa diğeri güvende). §5.3 kurtarma/yarım-yazma
  teşhisi **korunur ve güçlenir** (app_key_ref'in NULL olup olmaması, step 8'in
  geçilip geçilmediğinin DB tarafı tanığıdır). Ve bu, `aes_key_ref`'in kanıtlanmış
  deseninin (migration 00004/00013/00022) **birebir tekrarıdır** — yeni bir
  mekanizma icat etmez.

## Sonuçlar

- **Migration 00023:** `tags.app_key_ref bytea` (nullable, yaz-bir-kez trigger,
  44-bayt zarf CHECK'i, düz anahtar yasağı yorumu). **GRANT eklenmez ve bu
  bilinçlidir:** değer INSERT'te (satır yaratılırken) yazıldığı için `tappa_app`'in
  tablo-düzeyi INSERT'i yeni kolonu zaten kapsar; `UPDATE (app_key_ref)` grant'ı
  YAPILMAZ (yaz-bir-kez'in yapısal yarısı — `aes_key_ref` emsali, §4.7 en az yetki),
  ve `SELECT` de verilmez (sarmalı anahtar sqlc ile hiç dönmez; okuma gerekince
  tenant-kapsamlı ayrı bir yolla eklenir). `aes_key_ref` NOT NULL kalır;
  `app_key_ref` nullable çünkü 00023-öncesi
  satırlar onsuz doğdu — yeni akışta satır yaratılırken (step 3) `aes_key_ref` ile
  **birlikte** yazılır (§5.2 DB-önce), böylece step 8 çipe dokunmadan çok önce DB'de olur.
- **Encode driver:** step 8 exchange'i (`ChangeKey(0x00)`, en son, `changekey.go`
  case 2, MAC'siz `9100` kabulü — yanıt `EV2UnwrapResponseFull`'e verilmez);
  `session.go` `K_AppMaster` slotu step 3'te doldurulur ve `app_key_ref` step 3'te
  yazılır. `TestDriver_NoChangeKeyIsEverEmittedForApplicationKeyZero` **artık
  geçersiz** ve step 8'in doğru sırada (ChangeFileSettings'ten SONRA) emit
  edildiğini kilitleyen bir testle **değiştirilir**.
- **Fake chip:** anahtar-0-sonrası **oturum ölümü** modellenir (`chip_test.go`'nun
  bugün çözmediği, kendi yorumunda işaretli nokta).
- **ADR 0017 §6 md. 5 KAPANIR;** §5.1 step 8 artık sevk edilir. 0017'nin metnine
  dokunulmaz (immutable); bu ADR onu karara bağlayan kayıttır.
- ⚠️ **Kod + fake-chip testi bu turun kapsamıdır; GERÇEK bir çipte step 8 koşmak
  ayrı ve geri-dönülemez bir adımdır** (çip fabrika anahtarını kaybeder) — kullanıcı
  kararıyla, donanım kullanıcıda.
