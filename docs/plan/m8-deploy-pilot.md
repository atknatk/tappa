# M8 — Deploy & pilot

**Amaç.** Ürünü sahaya çıkarmak: tek VPS (AB bölgesi) + managed Postgres,
plaketlerin fiziksel encode'u ve KF St Julians'ta BioTime ile paralel pilot.

**Bittiğinde:** Gerçek çalışanlar gerçek plaketlere dokunuyor, kayıt karşılaştırma
tablosu çıkmış durumda (satış slaytı da olur).

---

## M8-01 — Paketleme

- **Bağımlılık:** M6 tamam
- **Commit:** `build: add production packaging`

**Kabul kriterleri.**
- `make build` → `CGO_ENABLED=0`, statik, `-trimpath`, sürüm/commit gömülü.
- Statik varlıklar ve şablonlar binary içinde (`web/embed.go`) — runtime dosya
  bağımlılığı yok.
- Migration ayrı adım (`tappa_owner`), uygulama başlangıcında **otomatik migrate yok**
  (yanlış rolle çalışma riski).
- Sağlık uçları: `/healthz` (canlı) + `/readyz` (DB'ye bağlı).
- `time/tzdata` gömülü — sistem tzdata'sı olmayan imajda vardiya hesabı çökmesin.

> **Kart düzeltmesi (2026-08-15, M8-01 uygulaması sırasında).** Beş kriterin **üçü
> zaten sevk edilmişti** ve kart bunu bilmiyordu; ölçümler (hepsi `5a86f9b`'de,
> uygulamadan **önce**):
>
> | Kriter | Ölçüm | Kartın bilmediği |
> |---|---|---|
> | `CGO_ENABLED=0` · `-trimpath` | `Makefile:72-73` | — |
> | sürüm/commit gömülü | `go version -m bin/tappa` → `vcs.revision=5a86f9b9…`, `vcs.time`, `vcs.modified` | **Zaten gömülüydü.** Go'nun `-buildvcs`'i yapıyor ve `-s -w` onu **soymuyor**; eksik olan **okuma**ydı (`grep -rn "Version\|BuildCommit" cmd/ internal/config/` → **0 eşleşme**). `-ldflags -X` **gereksiz**. |
> | runtime dosya bağımlılığı yok | üretim kodunda `os.ReadFile`/`os.Open`/`http.Dir` → **0 eşleşme** | Karşılanıyordu, ama **hiçbir şey tutmuyordu**. |
> | otomatik migrate yok | `go list -deps ./cmd/tappa \| grep -c goose` → **0** | Aynı: karşılanıyordu, tutan yoktu. |
> | `time/tzdata` gömülü | `go list -deps ./cmd/tappa \| grep -c '^time/tzdata$'` → **1** | 🔴 Ama import **`internal/domain/tap/tzdata.go:23`**'teydi — yani *tap karar motorunun* bir dosyası. `cmd/tappa`'nın bağımlılıkları değişse ya da o satır silinse **sessizce kaybolurdu**. |
>
> **Bu yüzden görevin ağırlığı "yapmak" değil, "bozulunca kırmızı vermesini
> sağlamak" oldu** (`cmd/tappa/packaging_test.go`, `cmd/tappa/serving_test.go`).
> Gerçekten yeni olan üç şey: **(a)** `cmd/tappa` `time/tzdata`'yı **kendi** import
> ediyor (idempotent; artık artefaktın kendi garantisi), **(b)** derleme kimliği
> `internal/buildinfo` ile **okunuyor** ve **açılışta tek satır** log'lanıyor —
> kirli ağaçtan derlenmişse **WARN** (bir commit'e geri izlenemez), **(c)**
> `/readyz` var (`internal/handler/health.go`) ve `/healthz` artık **HEAD**
> cevaplıyor.
>
> **Kartın söylemediği üç karar, ölçümleriyle:**
> 1. **Sürüm/commit nereye çıkacak?** Ölçüldü ve **herkese açık uca konmadı**:
>    `/healthz` gövdesi tam olarak `ok`, `/readyz` gövdesi `ready`/`not ready` —
>    ikisini de **tam eşitlikle** tutan testler `TestHealthz_AnswersGetAndHead` ve
>    `TestReadyz_AnswersTheDatabasesAnswer`. Commit hash'i kimliksiz bir uçta basmak,
>    saldırgana **hangi kodun** koştuğunu söyler; §4.7'nin sır listesinde değil ama
>    bedava da değil. Operatörün **iki ayrıcalıklı** kanalı zaten var: açılış log
>    satırı ve artefaktın kendisi (`go version -m`). Aynı gerekçeyle `-version`
>    bayrağı **eklenmedi** — `go version -m` aynı beş alanı zaten basıyor.
>    ⚠️ **Sürüm ≠ commit:** `git tag` **boş**, yani semver yok; toolchain'in
>    türettiği sözde-sürüm (`v0.0.0-20260815011242-5a86f9b9bb53`) dürüst cevaptır.
> 2. **`/readyz` `marketing.go`'nun "ölçümsüz" argümanını kırıyor mu?** Evet, ve
>    argüman **yeniden yazıldı** (o dosyada dipnot): `/healthz` ve `/static/*`'ın
>    sınıfı *"hiçbir şeye dokunmuyor"*, `/readyz` ise **havuza dokunuyor**. Limiter
>    **konmadı** (adres başına harita, 100k'da tamamen sıfırlanıyor — kimliksiz bir
>    uçta saldırgan sayısıyla büyüyen bellek); yerine **önbellek**: 200 eşzamanlı
>    istek → **tam 1** `Ping` (pozitif kontrol: pencere sıfırlanınca 200 → 200;
>    `TestReadyz_ABurstCostsOneQuery`), ve kesinti başına **1 ERROR + 1 INFO** satır
>    — durum **değişimi** log'lanıyor, durum değil (`TestReadyz_AnOutageCostsTwoLogLines`).
>    Ayrıca çağıranın iptali probe'a **geçmiyor** (`context.WithoutCancel`): aksi
>    halde sekmesini kapatan biri bu sürecin hazır-değil kanısını çevirip log'unu
>    doldurabilirdi (`TestReadyz_ACallerWhoHangsUpCannotFlipReadiness`).
> 3. **§4.5: hangi rol, hangi tenant bağlamı?** `pgxpool.Ping` **hiçbir tablo
>    okumuyor**. Ölçüldü (`TestReadyz_ATableReadCouldNotAnswerThisQuestion`, gerçek
>    Postgres): `SELECT count(*) FROM transactions` → **owner 200 473 satır**,
>    `tappa_app` **tenant bağlamı olmadan 0**. Yani tablo okuyan bir readiness
>    kontrolü **sağlıklı** bir veritabanında da 0 döner — başarısı hiçbir şey
>    kanıtlamaz. §4.7: 503 gövdesi **sabit**; sürücünün gerçek hatası (ölçüldü:
>    ``failed to connect to `user=… database=…`: 127.0.0.1:1 …``) yalnız **süreç
>    log'una** gidiyor.
>
> **2. tur — denetim yirmi mutasyon koşturdu, yedisi yeşil döndü ve altısı aynı
> sınıftı: argüman taşıyan bir sabiti/sırayı hiçbir şey tutmuyordu.** Kapatıldı ve
> her biri için mutasyonun artık kırmızı verdiği gösterildi: `defaultReadyTTL`
> (1 sa yapılınca ölü DB'de bir saat `ready` derdi) ve `defaultProbeTimeout` (20 dk
> yapılınca asılı DB'de her çağıran kilitte beklerdi — eski test **kendi** 50 ms'ini
> set ettiği için varsayılanı hiç sürmüyordu) artık **davranışla** pinli;
> `db.Ping`'in tablo okumadığı **bakım veritabanına** (`postgres`) karşı ölçülüyor —
> orada `transactions` **yok**, yani tablo okuyan bir ping 42P01 ile düşer;
> `logBuild`'in **DB dial'ından önce** koşması, bozuk DSN'li bir açılışın çıktısında
> kimlik satırının bulunmasıyla tutuluyor; `nosniff` artık **karşılaştırılmıyor,
> iddia ediliyor** (GET==HEAD karşılaştırması iki boş dizeyi eşit sayıyordu).
>
> **Ve tzdata kriteri "binary'de var"dan "gerçekten çözüyor"a yükseldi
> (2026-08-15, ölçüm):** `/usr/share/zoneinfo` **olmayan** `alpine:3` içinde, yalnız
> blank import'la ayrılan iki program — importsuz **üç zone'da da FAIL** (`unknown
> time zone Europe/Malta`, exit=3), importlu **hepsi OK** (`04:00 local = 02:00 UTC`,
> exit=0). Komut ve çıktının tamamı `cmd/tappa/packaging_test.go`'da, ilgili testin
> yanında **yeniden üretilebilir** biçimde yazılı. **Süite eklenmedi**, ve gerekçe
> ölçüm: bu repoda hiçbir test docker çalıştırmıyor (`grep` → 0), macOS'ta üretilen
> artefakt darwin/amd64 olduğu için **ikinci bir çapraz derleme** gerekiyor ve
> `alpine:3` **çekilmek zorunda kaldı** — `make test`'in bugün hiç taşımadığı bir ağ
> bağımlılığı. (CI docker koşuyor — `make up` — yani ileride eklenebilir; bu bilinçli
> bir genişletme kararı olur.)
>
> **3. tur — `tappa-security-auditor` ONAY; kapatılan tek madde yapısaldı.** Denetçi
> ölçtü: `handler.NewHealth(data, …)` ile **somut `*db.DB`** ilk kez
> `internal/handler`'a giriyordu. Tüketici arayüzü tek metotluydu ama **arayüz değeri
> nesnenin tamamını taşır**, ve o sarmalayıcı **tenant bağlamsız çözücüleri** de
> sunuyor (`internal/db/resolve.go`) — üstelik paket `internal/db`'yi zaten import
> ediyor (`adminreset.go`), yani `h.probe.(*db.DB).GetAdminSessionByTokenHash(…)`
> yazmanın önünde yalnız **disiplin** vardı. Çözüm: `readinessProbe` artık bir
> **fonksiyon tipi**, wiring `handler.NewHealth(data.Ping, …)`. Bağlı metot **tek bir
> çağrı** taşır, geri dönülecek bir dinamik tip yok. **İki ağ, ölçüldü (2026-08-15):**
> havuzu geri geçirmek **derlenmiyor** (`cannot use data (variable of type *db.DB) as
> handler.readinessProbe value`, `go build` exit=1) · `Health`'e bir `*db.DB` **alanı**
> eklemek derleniyor ama `TestHealth_HoldsACapabilityAndNotAPool` kırmızı veriyor.
>
> ### Devir — bu kartta kapatılmayanlar (ölçümleriyle)
>
> 1. **`Allow` iki ayrı başlık satırı olarak gidiyor, ve `OPTIONS` cevaplanmıyor.**
>    Ölçüm (2026-08-15, **gerçek artefakt**, `curl -s -o /dev/null -D -`):
>    `POST|PUT|OPTIONS|DELETE /readyz` ve `POST /healthz` → **405**, gövde 0 bayt,
>    başlıklar **`Allow: GET`** *ve ayrı satırda* **`Allow: HEAD`**.
>    ⚠️ **Denetim raporu bunu *"`Allow: GET`, HEAD'i saymıyor"* diye kaydetmişti;
>    yeniden ölçüm bunu düzeltti** — HEAD **ilan ediliyor**, yalnızca virgülle
>    birleştirilmiş tek satır olarak değil. Kusur bu yüzden daha dar: *"ilk `Allow`
>    başlığını oku"* diyen istemciler (Go'nun `Header.Get`'i dahil) yalnız `GET`
>    görür, ve `OPTIONS` 405 alır (204 + `Allow` değil). chi'nin varsayılan davranışı;
>    **bu diff'in ürettiği bir kusur değil** — `/` de aynı iki satırı veriyor (M7-01).
>    → **M8-02**: router'a virgüllü tek `Allow` üreten ve `OPTIONS`'ı cevaplayan bir
>    `MethodNotAllowed` handler'ı.
> 2. **`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir WARN.** Bu
>    turun kendi artefaktı öyle: `version=v0.0.0-…+dirty … modified=true`. Sinyal
>    **var, filtrelenebilir ve testle tutuluyor**, ama `TAPPA_ENV=prod` ile kirli bir
>    binary'yi **reddeden ne bir kod ne bir test** var. → **M8-02** runbook adımı ya da
>    `make build` kapısı.
> 3. **Kilit probe boyunca tutuluyor.** 3. tur denetimi ölçtü (asılı DB, soğuk
>    önbellek, **300 eşzamanlı istek**): hepsi 503, **maks 2,001 sn** — yani
>    `defaultProbeTimeout` fiilen bağlıyor — ve aynı anda `/healthz` **0,0008 sn**.
>    Tetikleyici **gerçek bir kesinti**; saldırgan üretemez (önbellek + `WithoutCancel`)
>    ve pencere kapanınca herkes önbellekten anında döner. → **M8-03** (alarm/
>    gözlemlenebilirlik), davranış değişikliği değil.
> 4. **⚠️ DOĞRULANAMADI, ne bulgu ne temiz:** *"havuz doyduğunda `/readyz` 503 verip
>    tüm replikaları rotasyondan düşürür"* zinciri **ölçülemedi**. Denetçi
>    `pool_max_conns=1` + `LOCK TABLE` ile denedi; **kimliksiz hiçbir istek yolu havuz
>    bağlantısını ≥2 sn tutmuyor** (`/t` → 400/303, ikisi de <2 ms, DB'ye hiç
>    gitmeden). Yani bugün bu senaryoyu **kurmanın yolu bulunamadı**; kapasite
>    planlaması yapılırken (M8-02) yeniden bakılmalı.
>
> **T29'un `/healthz` yarısı bu kartta kapatıldı** (uptime monitörleri HEAD'ler ve
> 405'i *"servis bozuk"* okur). `/t`, `/activate` ve panel **hâlâ 405** — farklı
> patlama yarıçapı, T29'da açık kalıyor. **T28 (HSTS) ve T30 (`TAPPA_TRUSTED_PROXIES`)
> bu kartta ele alınmadı:** ikisi de **dağıtım** kararı (TLS'i nerede sonlandırdığımız,
> ters vekilin CIDR'i) ve yeri **M8-02**'nin runbook'u.

---

## M8-02 — Barındırma

- **Bağımlılık:** M8-01 · Q08 · Q12
- **Commit:** `docs(ops): document deployment runbook`

**Kabul kriterleri.**
- VPS + managed Postgres, **AB bölgesi**, aylık maliyet hedefte (~€30-50).
- TLS (HSTS dahil); SUN URL'si `https://` — domain Q08 ile netleşmiş.
- `tappa_app` / `tappa_owner` rol ayrımı üretimde de geçerli; uygulama
  **asla** migration rolüyle bağlanmıyor.
- Yedek: otomatik, geri yükleme **denenmiş** (denenmemiş yedek yedek değildir).
- Sırlar ortam değişkeninde, repoda değil; KEK dönme prosedürü yazılı **ve
  yürütülebilir**: tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan
  araç var. Yürütülemeyen bir runbook, KEK sızıntısı anında hiçbir işe yaramaz.
- **Deploy penceresi vardiyalara göre seçiliyor.** KM Pastry & Bakery 04:00'te,
  Meat Production 05:00'te giriş yapıyor; sunucu kapalıysa sayfa hiç yüklenmez
  ve çevrimdışı kuyruk (M9-01) bile devreye giremez — §4.6 altyapı katmanında
  karşılıksız kalır. Kesintisiz deploy ya da ilan edilmiş bakım penceresi.
- **DPA ve saklama politikası (Q23):** Tappa↔müşteri veri işleme sözleşmesi
  (KF/KM veri sorumlusu, Tappa veri işleyen), saklama süreleri ve silme/
  anonimleştirme akışı (Q13) yazılı ve imzalanmış.
- Runbook: deploy, rollback, migration, olay müdahalesi.

---

## M8-03 — Gözlemlenebilirlik

- **Bağımlılık:** M8-02
- **Kırmızı çizgi:** §4.7 · §7
- **Commit:** `feat(ops): add structured logging and alerts`

**Kabul kriterleri.**
- `log/slog`, yapılandırılmış, request id korelasyonlu.
- **Asla loglanmayanlar** doğrulandı: oturum token'ı, `token_hash`, CMAC, AES
  anahtarı, davet kodu, tam GPS koordinatı.
- Uyarı kuralları: `reject` oranı sıçraması, `flag` kuyruğu birikmesi, deaktive
  hesap denemesi, şüpheli ctr sıçraması, 5xx oranı.
- Prod log'ları AB'de, saklama süresi belirli.

---

## M8-04 — Güvenlik denetimi

- **Bağımlılık:** M8-01
- **Kırmızı çizgi:** §4 (tamamı)
- **Commit:** `chore(security): address pre-pilot audit findings`

**Kabul kriterleri.**
- agent `tappa-security-auditor` tam repo üzerinde koştu; R1–R8 için kanıtlı rapor.
- `make audit` temiz: `govulncheck` + `scripts/redline-check.sh`.
- KRİTİK ve YÜKSEK bulgu **kalmadı**; ORTA/DÜŞÜK olanlar ya kapandı ya
  gerekçesiyle kabul edildi ve yazıldı.
- Manuel doğrulamalar: replay denemesi gerçek etiketle, çapraz-tenant erişim
  denemesi, oturum çalma senaryosu, oran sınırı.
- Mekanik taramanın **kanıt olmadığı** unutulmadı — derin denetim ayrı yapıldı.

---

## M8-05 — Plaket encode runbook

- **Bağımlılık:** M2-01 · Q06 · Q10
- **Kırmızı çizgi:** §4.7
- **Commit:** `docs(ops): document tag encoding runbook`

**Amaç.** Fiziksel plaketleri üretime hazır hale getirmek.

**Kabul kriterleri.**
- Encode ayarları yazılı: UID mirror + counter mirror açık, SDM MAC input offset,
  dosya okuma izni açık, **yazma izni anahtarla kilitli**.
- Geliştirme akışı: 10'luk NTAG 424 DNA paketi (~30 €) + NXP TagWriter.
- **En az bir fiziksel etiketle uçtan uca doğrulama yapıldı** — test vektörleri
  yeşil ama gerçek çip kırmızı olabilir; bu adım atlanamaz.
- Anahtar teslimi ve döndürme prosedürü (tedarikçiden gelen anahtarlar üretimde
  değiştirilir).
- Anahtarlar repoya, sohbete, e-postaya **yazılmadı**; KEK ile sarmalı DB'de.
- Plaket baskısı: A5, NFC + QR birlikte, kamera görüş alanına montaj notu.

---

## M8-06 — KF St Julians pilotu

- **Bağımlılık:** M8-02 … M8-05 · Q13
- **Commit:** `docs: record pilot results`

**Amaç.** Tek şubede, 1 hafta, **BioTime ile paralel** çalıştırma.

> **PİLOT KAPISI (Q23) — hepsi ✓ olmadan pilot başlamaz.** Pilot gerçek
> çalışanların gerçek konum ve IP verisiyle koşacak:
> - [ ] GDPR Art. 13 çalışan aydınlatma metni yayında ve aktivasyonda gösteriliyor ([M5-02](m5-tap-akisi.md))
> - [ ] Gizlilik politikası, hizmet şartları, imprint yayında ([M7-01](m7-portal.md))
> - [ ] Tappa↔KF veri işleme sözleşmesi (DPA) imzalı ([M8-02](m8-deploy-pilot.md))
> - [ ] Saklama süresi ve silme/anonimleştirme akışı karara bağlı (Q13)
> - [ ] Telefon envanteri çıkarıldı ([M8-07](m8-deploy-pilot.md))
> - [ ] Üretim tenant'ı kuruldu ve doğrulandı ([M8-07](m8-deploy-pilot.md))
>
> "BioTime'ın GDPR yükünden kurtulun" diye satılan bir ürünün kendi pilotunda
> aydınlatma metni olmadan çalışması, ürünün ana satış argümanını çürütür.

**Kabul kriterleri.**
- Pilot kapısındaki altı madde de ✓.
- 15 kişilik kadro aktive edildi; her çalışanın practice tap'i tamamlandı.
- 1 hafta boyunca iki sistem paralel; günlük kayıt karşılaştırma tablosu.
- Uyuşmazlıkların her biri açıklandı (kim, ne zaman, neden) — açıklanamayan
  fark **kalmadı**.
- `flag` oranı ve nedenleri raporlandı — politika `sid` kırılımıyla (M3-07).
- **IP eşleşme oranı ölçüldü (Q14).** Kaç tap mekânın ağından geldi, kaçı mobil
  veriyle? Oran düşükse `proof of place` pratikte GPS'e indirgenmiş demektir;
  trust ağırlıkları (50/30) ve `base:gps-only-allow` varsayılanı buna göre
  yeniden değerlendirilir.
- **GPS-only tap oranı** ve WiFi adımını atlayan çalışan sayısı raporlandı.
- Çalışan geri bildirimi toplandı (ekran anlaşıldı mı, kaç saniye sürdü).
- Q13 (GDPR saklama/silme politikası) karara bağlandı ve yazıldı.
- Sonuç: iki firmaya tam yayılım kararı → BioTime kapatılır.

**Tuzaklar.**
- Pilot sırasında kod değiştirmek karşılaştırmayı bozar. Kritik hata dışında
  değişiklik pilot bitiminde.

---

## M8-07 — Üretim tenant kurulumu ve cihaz envanteri

- **Bağımlılık:** M7-03 · M8-02
- **Kırmızı çizgi:** §4.5 · §4.7
- **Commit:** `docs(ops): document production tenant setup`

**Amaç.** Pilotun üzerinde koşacağı **gerçek** tenant'ı kurmak ve sahadaki
telefonların ne yapabildiğini önceden bilmek.

**Neden.** Denetimde çıktı: M1-10 seed'i açıkça **sahte** (dokümantasyon IP'leri,
sahte AES anahtarları). M8-06 "15 kişilik kadro aktive edildi" diyor ama gerçek
KF tenant'ının hangi araçla, hangi adımlarla kurulacağı hiçbir kartta yoktu.

**Adımlar.**
1. KF tenant'ı M7-02 kayıt sihirbazından **gerçek veriyle** açılır (seed ile değil).
2. 9 lokasyon, gerçek statik IP'ler ve GPS koordinatları girilir; Rusty Bar
   `overnight = true`.
3. Her lokasyonun WiFi SSID'si lokasyon kaydına girilir ([M5-02](m5-tap-akisi.md)
   aktivasyon adımında çalışana gösterilecek).
4. 15 çalışan profili girilir; davetler gönderilir.
5. Plaketler encode edilir ([M8-05](m8-deploy-pilot.md)) ve UID'ler lokasyonlara bağlanır.
6. **Telefon envanteri:** kadroya tek soruluk anket — telefon modeli. Amaç:
   arka planda NFC okuyamayan cihazları (iPhone X ve öncesi) saymak.

**Kabul kriterleri.**
- Üretim tenant'ında **hiçbir** seed verisi yok; sahte AES anahtarı, dokümantasyon
  IP'si veya `91AC7E55…` tarzı örnek UID bulunmuyor.
- Statik IP'ler müşteriden **teyitli** (ISS yazısı veya test tap'i ile doğrulandı).
- NFC okuyamayan telefon oranı biliniyor. Yüksekse ([M5-08](m5-tap-akisi.md)):
  o çalışanlar kalıcı olarak QR yolunda demektir ve Q15 gereği IP zorunlu —
  WiFi adımı onlar için kritik, gerekirse tenant bazında politika gözden geçirilir.
- Gerçek bir plaketle, gerçek bir telefonla **en az bir uçtan uca tap** yapıldı
  ve kaydı panelde görüldü.
- Kurulum adımları runbook'a yazıldı — ikinci tenant (KM) aynı adımlarla açılacak.

**Tuzaklar.**
- Gerçek statik IP'leri ve müşteri verilerini **repoya yazma** (§4.7 ruhu;
  `tappa-seed` skill'i dokümantasyon aralıklarını şart koşuyor).
