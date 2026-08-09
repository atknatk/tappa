# Backlog — kullanıcı eylemi bekleyen işler

> **Bu dosya yalnız KULLANICININ yapabileceği işleri tutar** — fiziksel cihaz gerektiren
> ölçümler, hesap/abonelik kararları, sudo isteyen kurulumlar, dış servis kayıtları.
> Ajan bunları kodla kapatamaz.
>
> **Bu bir durum dosyası DEĞİL.** Projenin canlı durumu tek yerdedir:
> [docs/plan/state.md](plan/state.md) (CLAUDE.md §12). Buraya görev durumu, ledger
> satırı veya "sıradaki iş" yazma — çelişirler. Buradaki bir madde bir görevi
> bloklamıyorsa bunu açıkça söyler.
>
> Kullanıcı **"backlog ekle"** dediğinde madde buraya eklenir.

---

## Açık

### B1 — iOS Safari çerez ömrü ölçümü (Q11)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q11, M5-01 (`a71e1b2`), M8-05

**Ne yapılacak.** Gerçek bir iPhone'da uçtan uca çerez ömrünü ölç:

1. Plakete dokun → Safari açılır → aktive ol (çerez yazılır).
2. **Günler/haftalar** bekle — arada Safari'yi normal kullan (ITP'nin tetiklenmesi için).
3. Aynı telefonla tekrar dokun → **hâlâ tanınıyor musun?**

**Neden önemli.** Ürünün "telefon seni tanır" vaadi buna dayanıyor. Çalışan uygulama
kurmuyor; tek kimlik kanıtı bu kalıcı çerez (§5'in "kim" kanıtı).

**Teknik olarak sınanan şey.** Safari ITP, **JavaScript ile yazılan** çerezleri 7 güne
kırpar. **Sunucunun `Set-Cookie` ile yazdığı `httpOnly` çerez** bu kırpmaya tabi
*olmamalı* — M5-01 tam olarak öyle yazıyor (`httpOnly`, `SameSite=Lax`,
`Max-Age=31536000`). Ölçüm bu ayrımın gerçekte tuttuğunu doğrular.

**M5-01'i bloklamadı, çünkü:** sunucu tarafı sonuçtan bağımsız — ölçüm ne çıkarsa
çıksın kod aynı. Bu yüzden bir kabul kriteri olamaz (kart düzeltmesi, 2026-07-31).
Sonuç çıkınca: [open-questions.md](plan/open-questions.md) → Q11'e yaz.

**Sonuç kötü çıkarsa** (çerez erken düşüyorsa) seçenekler: aktivasyonu daha sık
tekrarlatan bir akış, ya da çereze ek olarak ikinci bir tanıma kanıtı — **ama §4.1
gereği biyometri/WebAuthn/attestation değil.** Karar o zaman verilir.

### B3 — Saklama süresinin hukuki onayı (Q13)

**Durum:** açık · **Bloklar:** hiçbir şeyi (kod hazır) · **İlgili:** Q13, M5-02, M8-06

**Ne yapılacak.** `TAPPA_RETENTION_YEARS`'in **gerçek** değerini bir hukukçuya doğrulat
(Malta/AB istihdam + bordro/vergi saklama yükümlülüğü) ve `.env`'e yaz.

**Neden önemli.** Bu sayı **GDPR Art. 13 aydınlatma metninde çalışana gösteriliyor** —
yani hukuki bir beyan. Yanlışsa metin çalışana yanlış bilgi vermiş olur.

**Kod tarafı hazır ve bu yüzden bloklamıyor** (kullanıcı kararı 2026-07-31): sayı
**koda gömülmedi**, config'den geliyor; aydınlatma metni onu render ediyor. Dev/demo
değeri bir **varsayılan**dır, hukuki iddia değildir — kod bunu böyle söylüyor.
Doğru değer öğrenilince tek yapılacak `.env`'i güncellemek; sürüm çıkmaya gerek yok.

**Sonuç çıkınca:** [open-questions.md](plan/open-questions.md) → Q13'e yaz ve buradaki
maddeyi "Kapananlar"a taşı. Q13 ayrıca "GDPR silme talebi × immutable `transactions`"
sorusunu da taşıyor (M8-06) — saklama süresi onun bir parçası, tamamı değil.

### B2 — arm64 Go toolchain kurulumu (Q26)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q26, M0-07

**Ne yapılacak.** Yerel Go toolchain'i arm64'e geçir. Tarball indirildi ve checksum'ı
go.dev ile **birebir doğrulandı** (`efb87ff2…`), ama `/usr/local`'a kurulum **sudo
parolası** istiyor — kullanıcı çalıştırmalı. Komutlar
[state.md](plan/state.md) oturum notunda.

**Kazanç yalnız hız.** Her şey bugün amd64 Go 1.26.5 ile yeşil; Rosetta altında derleme
~2-3x yavaş. Kurulunca `go version` → `darwin/arm64` olur ve ilk `make gen` bir kez
uzun sürer (build cache + pinli CLI önbellekleri tazelenir — bozukluk değil).

---

## Ertelenmiş teknik borç (sahibi bir görev kartı DEĞİL)

> Bunlar **kullanıcı eylemi değil**, ama hiçbir yakın görev kartının da sahiplenmediği işler —
> çoğu dağıtım (M8) zamanına ait. Buraya yazılmalarının sebebi kaybolmaları: M8 uzak.
>
> ⚠️ **Görev-kapsamlı devirler buraya YAZILMAZ** — onlar [state.md](plan/state.md)'deki
> "M5-04/M5-05'e devralınan" gibi bölümlerde ve sahibi bellidir (ör. 429'un §4.6 kalıntısı → M5-04,
> N5 tenant-mismatch → M5-05, font self-host → M5-04). İki yere yazılan gerçek çelişir.

| # | Ne | Nereden | Sahibi |
|---|---|---|---|
| **T1** | **Oran sınırlayıcı süreç-içi ve sabit-pencere.** İki instance sınırı ikiye katlar; pencere sınırında kısa sürede 2×limit mümkün; `limiterMaxKeys` (100k) aşılınca map **toptan sıfırlanıyor** (fail-open, bilinçli ve yazılı). Paylaşılan store gerekir. | M5-02, M5-03 | M8 |
| **T2** | **`TAPPA_TRUSTED_PROXIES` dağıtım kontrol listesi.** Proxy XFF'e **append** etmeli — **replace** eden proxy koddan ayırt **edilemez**. Kapı yalnız tek girdilik `/0`'ı yakalar; `/1` veya birleşimle tüm uzayı kaplayan liste (`0.0.0.0/1,128.0.0.0/1`) yakalanmaz. Güvenilen aralık **gerçek istemcileri içeriyorsa** onlar serbestçe IP uydurabilir (ölçüldü). | M5-03 | M8 |
| **T3** | **Çerez gölgeleme + `__Host-` öneki.** Aynı isimli iki `tappa_activation` çerezinde `r.Cookie` **ilkini** alıyor; alt alan adı kontrolü gerektirir. | M5-02 | M8 |
| **T4** | **`docker-compose.yml` `log_statement=all` bind parametrelerini log'a yazıyor** (denetçi 30 dk'da `$1 = '<64-hex>'` biçiminde **1036 satır** saydı). `code_hash` taşıyıcı kimlik bilgisi olduğu için dev Postgres log'u bir aktivasyon anahtarı deposuna dönüşüyor. **Dev-only** ve denetçiler bu log'u ölçüm aracı olarak kullanıyor → bilinçli bırakıldı; üretim deploy config'i repoda henüz yok. | M5-02 | M8 |
| **T5** | **`config.Load` `TAPPA_BASE_URL`'ü doğrulamıyor.** `NewCookies` bir **prefix testi** yapar, URL parse etmez → başında boşluk olan veya URL olmayan değer NOT-Secure dalına düşer (**non-prod'la sınırlı**; prod koşulsuz Secure). | M5-01, M5-02 | M5-03 sonrası / M8 |
| **T6** | **Dev-DB test kalıntısı birikiyor.** `tenants` ~7250 satır; `audit_log`/`transactions` append-only olduğu için **tasarımca** temizlenemez (M1-09: imkânsızlık = garanti). Kırmızı çizgi değil, hijyen. Demo/prod öncesi `make db-reset`; kalıcı çözüm testcontainers ile izole DB. | M3-02, M5-03 | M8 |
| **T7** | **`aes_key_ref` KEK-sarmalı doğrulaması — DARALDI (M5-09), kapanmadı.** Seed yarısı **kapandı**: `make seed` artık gerçek 44 baytlık zarf yazıyor ve bir **drift guard** (`RAISE`) sarmalanmamış demo plaketi bırakırsa seed'i düşürüyor (mutasyonla kanıtlı). Açık kalan: şema `bytea`ya "bu bir KEK zarfıdır" dedirtemiyor, yani **insert yolu** (plaket kaydı/değişimi) hâlâ disipline bağlı. ⚠️ Ölçüldü: şema en azından **şekli** zorlayabilirdi — `CHECK (octet_length(aes_key_ref) = 44)`; bugün engelleyen tek şey iki adımlı seed'in **50 baytlık ara durumu** (adım 1 placeholder yazar, adım 2 sarmalar). Üç seçenek M5-09 kartında yazılı. | M1-05, M5-09 | M6/M7 (plaket kayıt akışı) |
| **T8** | **🔴 `tags.uid` CHECK'i İKİ YAZIMA izin veriyor, zarfın AAD'si ise HAM 7 BAYT.** `hex.DecodeString` büyük/küçük harf duyarsız + `CHECK (uid ~ '^[0-9A-Fa-f]{14}$')` → `04AC7E55000601` ile `04ac7e55000601` **iki ayrı PK satırı, aynı AAD**. Ölçüldü: bir plaketin zarfı diğerine **açılıyor**, `last_ctr`'ler **bağımsız**, çapraz-tenant ikinci satır INSERT **edilebiliyor**. **Bugün sömürülemiyor** (`sun.Parse` UID'yi büyük harfe kanonikleştiriyor; `tags`'a INSERT eden üretim yolu **yok**). Latent risk **M8-05**: operatörün yazdığı uid olduğu gibi eklenirse sonuç **ölü plaket** — kaydedilir, hiç tap almaz, hata vermez. Çözüm ikinci temsili **silmek**: yeni migration, `CHECK (uid ~ '^[0-9A-F]{14}$')` (00004 immutable). Mevcut 12 satır zaten büyük harf → veri taşıması yok. | M5-09 güvenlik denetimi | **M8-05'ten ÖNCE** |
| **T9** | **🔴 `tappa_app` `tags`'ın DOKUZ sütununda da UPDATE'e sahip.** Ölçüldü (canlı, `BEGIN…ROLLBACK`): `aes_key_ref`'i bozabiliyor (DoS), `uid`'i yeniden adlandırabiliyor (DoS) ve **`last_ctr`'ı 0'a GERİ SARABİLİYOR** (§4.4 replay penceresi). Bugün erişilebilir değil — `tags` üzerinde tek sqlc sorgusu `AdvanceTagCounter`, ve repoda **dinamik SQL yok**. ⚠️ **Sütun-düzeyi grant TEK BAŞINA yetmez:** `last_ctr` listede kalmak zorunda (§4.4 advance onu yazar) ve en ağır yetenek tam olarak onu geri sarmak. Gerçek çözüm: `REVOKE UPDATE ON tags` + `GRANT UPDATE (last_ctr, status, retired_at, replaced_by)` **artı** monotonluk trigger'ı (`NEW.last_ctr > OLD.last_ctr`). | M5-09 güvenlik denetimi | M6/M7 |
| **T10** | **🔴 CI OLDUĞU GİBİ KIRMIZI VERİR VE BUGÜNE KADAR KİMSE GÖRMEDİ.** Repoda **uzak yok** (kullanıcı kararı: push/PR yok) → `ci.yml` **hiç çalışmadı**; "CI yeşil" teorik bir cümle. Olduğu gibi koşarsa: workflow `make up` koşuyor (yalnız Postgres'i **başlatır**) ama **`make migrate` koşmuyor**, `make seed` koşmuyor, `TAPPA_TAG_KEK` **vermiyor** → `make check` → `make test` migrate edilmemiş şemaya çarpar. Ölçüldü (boş DB): **17 pakette 140 üst düzey FAIL**, 136'sı M5-09'dan **eski** (M1-09'dan beri taşınıyor). Düzeltme tek satır değil (migrate + KEK + seed) ve **uzak olmadan doğrulanamaz**. O zamana kadar `make check`'in tek gerçek koşum yeri geliştiricinin makinesidir. | M5-09 güvenlik denetimi | M8 / M0-06 |
| **T11** | **`transactions(tenant_id, location_id)` indeksi yok ve bir EKRAN TASARIMINI belirledi.** M6-06 A'da lokasyon silme kontrolü dört referans sayımı istedi; `EXPLAIN (ANALYZE, BUFFERS)` (tenant `1000…0001`, 9 lokasyon, ~28.000 işlem): liste satırı başına dört sayım **288 ms** — tamamı `transactions` sayımı, **bitmap heap scan, ~44.000 heap bloğu**; tek lokasyon için **27–31 ms**. Kontrol bu yüzden listeden **düzenleme kartına** taşındı, liste **0 ms** ödüyor. ⚠️ **Bu ölçüm İKİ KEZ yanlış popülasyonla yazıldı** (biri tenant filtresiz tam tablo taraması): gerçek değişken referanssızlık değil **tenant'ın işlem hacmi** — aynı ifade **0,55 ms ile 242 ms** arasında geziyor. İndeks eklenirse liste-geneli şekil de uygulanabilir olur. **00013'e aday.** | M6-06 A | M6-06 B / M7 |
| **T12** | **MAC'li onay jetonu TEK KULLANIMLIK DEĞİL.** Aynı jetonla ikinci POST yine geçiyor (ölçüldü; bir denetim tek mint'i **üç kez** harcadı). Bugün zararsız, çünkü her kapının **kendi** ikinci savunması var: deaktivasyonda `status <> 'deactivated'`, kaldırmalarda **hiçbir satırla eşleşmeyen ikinci DELETE**. ⚠️ Bu, M6-06 A'da *"silme onayını imzala"* seçeneğinin **elenme gerekçesiydi** — imzalı bir iddiayı TTL içinde yeniden harcanabilir kılmak kusuru kapatmaz, **taşır**. Kapatmak harcanmış-değer tablosu **artı kendi saklama hikâyesini** ister → ayrı görev. `deactivateconfirm.go` bunu 12 parçalık listesinde **açık limit** olarak sayıyor. | M6-05 B, M6-06 A | M7 |
| **T13** | **`tappa_owner` `audit_log`'u TRUNCATE edebiliyor.** Append-only tetikleyici `BEFORE UPDATE OR DELETE` — **TRUNCATE'i kapsamıyor** (`tappa_app` edemiyor: yalnız SELECT+INSERT). 00005'in durumu, bu fazın değil; **ama bedeli M6-06 A ile değişti**: `audit_log` artık silinmiş bir lokasyonun **tek hayatta kalan kaydı** ve C′ silme bildiriminin **tek dayanağı**. Bir truncate onay **uydurmaz** — hepsini **susturur** (ekran zaten "hiçbir şey söyleme" yönüne düşüyor). | M5-09, M6-06 A güvenlik denetimi | M7/M8 |
| **T14** | **Onay jetonunu ÜRETEN GET, Origin kapısının dışında.** `?venue=<id>&confirm=remove` çapraz-origin bir üst düzey gezinmede jetonu basıp `Set-Cookie` yazıyor (`tappa_admin_confirm`, HttpOnly, SameSite=Lax, Path=/admin). Saldırgan gövdeyi **okuyamaz** (jetonu öğrenemez) ve POST Origin kapılı; kalan risk yöneticiye istemediği bir *"Remove X?"* uyarısını **gösterebilmek**. **Bu faza özgü değil** — M6-05 `employees.go` aynı şekli sevk etmişti. | M6-05 B, M6-06 A güvenlik denetimi | M7 |

---

## Kapananlar

*(henüz yok)*
