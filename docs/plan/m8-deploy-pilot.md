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

> **Kart düzeltmesi (2026-08-15, M8-02 uygulaması sırasında — PAKETLEME + KÜME
> MANİFESTLERİ yarısı).** Bu tur `Dockerfile`, `.dockerignore`, `deploy/k8s/*` ve
> `.github/workflows/deploy.yml`'i getirdi. **Kartın hedefi değişti ve bunu yazmak
> gerekiyor:** kart *"tek VPS + managed Postgres"* diyor; hedef **k3s v1.35.4, tek
> node** (`k8s-1.fsn1.private`, Hetzner fsn1, `144.76.158.60`) ve **düz Postgres**
> oldu (kullanıcı kararı, uyarıldıktan sonra).
>
> **Karşılanmayan iki kriter — sessizce kabul EDİLMEDİ, manifestin başına ve
> `deploy/README.md`'ye yazıldı:**
>
> | Kriter | Durum | Ölçüm |
> |---|---|---|
> | *"managed Postgres"* (§1) | **karşılanmıyor** | `kubectl get sc` → tek sınıf **`local-path`** (varsayılan, `WaitForFirstConsumer`, `reclaimPolicy: Delete`), replika yok |
> | *"Yedek: otomatik, geri yükleme **denenmiş**"* | **karşılanmıyor** | yedek **yok**; denenecek bir şey de yok. M8-06 pilotu bir haftalık paralel kayıt üretiyor ve o hafta §4.3 gereği yeniden kurulamaz → **pilot öncesi kapatılmalı** |
> | DPA / Q23, KEK döndürme aracı, runbook | **bu turda yok** | kartın ikinci yarısı |
>
> **🔴 EN AĞIR BULGU KARTTA HİÇ YOKTU: `everva.com.tr` CLOUDFLARE'DE VE JOKER KAYIT
> PROXY'Lİ.** Ölçüldü (2026-08-15, komutlarıyla):
> `dig +short tappa.everva.com.tr` → **`104.21.72.109`, `172.67.181.173`** (uydurma
> bir alt alan da aynı ikisi) · `dig +short NS everva.com.tr` → `*.ns.cloudflare.com`
> · `curl -sS -o /dev/null -D - https://argocd.everva.com.tr/` → **`server: cloudflare`,
> `cf-ray: …`**. Yani hedef host **bugün zaten** Cloudflare üzerinden cevap veriyor.
> ingress-nginx'in ConfigMap'i **boş** (`kubectl -n ingress-nginx get cm
> ingress-nginx-controller -o jsonpath='{.data}'` → boş), yani `use-forwarded-headers`
> varsayılan `false` ve nginx `X-Forwarded-For`'u `$remote_addr` ile **EZİYOR**.
> Proxy açık kalırsa uygulama her istemciyi bir **Cloudflare kenar adresi** olarak
> görür: §5'in IP kanıtı (50/100 güven puanı) **hiç kimse için** doğru olamaz, panel
> giriş bütçesi (T30, 120/10 dk/adres) **tüm müşteriler için tek adrese** çöker, ve
> T40'ın aynası doğar (Cloudflare aralığı girilmiş bir mekân tüm interneti
> *"ağ kanıtı"* sayar — üstelik değişmez bir satıra). **Karar: bu host için DNS-only
> (gri bulut), A → `144.76.158.60`.** Elenenler ve gerekçeleri `deploy/k8s/40-ingress.yaml`
> başında; paylaşılan ingress ConfigMap'ini değiştirmek ~20 başka uygulamanın istemci
> adresi çözümlemesini değiştirirdi.
>
> **T30 (`TAPPA_TRUSTED_PROXIES`) ÖLÇÜLDÜ, TAHMİN EDİLMEDİ → `10.42.0.1/32`.**
> ingress-nginx burada **`hostNetwork: true` bir DaemonSet** (`podIP=10.0.0.10`,
> hostPort 80/443), yani pod ağına node'un ağ ad alanından giriyor:
> `kubectl -n ingress-nginx exec ds/ingress-nginx-controller -- ip route get 10.42.0.5`
> → **`dev cni0 src 10.42.0.1`**. `10.42.0.1` cni0 köprüsünün kendisi; flannel pod
> adreslerini `10.42.0.2`'den dağıtıyor (podCIDR `10.42.0.0/24`), yani **hiçbir pod bu
> adresi alamaz**. `/24` yazmak kümedeki ~30 namespace'in her pod'una XFF sahteciliği
> yetkisi verirdi; `/32` yalnız bu node'un yönlendirme yolunu güvenilir sayar.
>
> **T28 (HSTS) — ölçüldü, ingress zaten gönderiyor.** Origin'e doğrudan (Cloudflare'ı
> atlayarak): `curl -sS -o /dev/null -D - -k --resolve argocd.everva.com.tr:443:144.76.158.60
> https://argocd.everva.com.tr/` → **`strict-transport-security: max-age=31536000;
> includeSubDomains`**. Bu Ingress onu miras alıyor. **`preload` set edilmedi** (geri
> alması aylar sürer) ve **uygulamanın kendi başlığını set etmesi hâlâ açık** — HSTS
> ingress-nginx'te per-controller bir ConfigMap seçeneği, per-Ingress annotation'ı
> **yok**, snippet annotation'ları da ≥1.9'da varsayılan olarak kapalı.
>
> **T31'in yarısı sevk edilen ikilide kapandı.** İmaj `golang:1.26.6-bookworm` ile
> derleniyor (CI hâlâ 1.26.5 pinli — o kullanıcının Go kurulumu). Ölçüldü:
> `go version -m` → `go1.26.6`. `make audit` **hâlâ kırmızı**, çünkü govulncheck
> geliştirici makinesinin toolchain'ini sayıyor.
>
> **Ölçerek verilen beş karar (brief'in *"ölç ve seç"* dediği yerler):**
> 1. **Nihai imaj `scratch`** — 22,3 MB (16,1 MB ikili + 216 KB CA paketi). Dosya
>    sistemi tam olarak iki dosya. **tzdata M8-01'in sondasıyla, ama `alpine:3`'te
>    değil SEVK EDİLEN İMAJIN İÇİNDE** kanıtlandı: importsuz kontrol **üç zone'da da
>    FAIL** (`unknown time zone Europe/Malta`, exit=3), importlu **hepsi OK**
>    (`04:00 local = 02:00 UTC`, exit=0). **`ca-certificates` GEREKLİ, ölçüldü:** aynı
>    ikili sertifikasız `scratch`'te `x509: certificate signed by unknown authority`,
>    sevk edilen imajda `VIES TLS: OK` (VIES çağrısı Q09 gereği best-effort, yani
>    eksik paket çökme değil **sessiz bozulma** üretirdi).
> 2. **Migration `Job`, initContainer DEĞİL.** Üç ölçülebilir fark: `tappa_owner`
>    DSN'i internete açık pod'un spec'ine girmez · initContainer **her** pod
>    başlangıcında koşar (CrashLoopBackOff = sıkı döngüde DDL denemesi) · N replika =
>    N eşzamanlı goose. Ayrıca `deploy.yml` Job'ı **bekliyor**, yani şema ilerlemeden
>    yeni ikili servis etmiyor.
> 3. **`replicas: 1`, ve bu bir varsayılan değil ölçüm.** `internal/domain/legal`
>    paket dokümanı: *"A SECOND process would serve the previous text until it
>    restarted."* İki replika = aynı hostname'den **iki farklı gizlilik politikası**.
>    `maxSurge:1 / maxUnavailable:0` kartın *"kesintisiz deploy"*ını karşılıyor.
> 4. **`deploy.yml` ayrı workflow (`workflow_run`), `ci.yml`'e job DEĞİL.** Güvenlik
>    gerekçesi: GitHub `workflow_run` iş akışlarını **daima varsayılan dalın**
>    kopyasından koşturur, yani bir PR bu dosyayı değiştirip koşturamaz. `ci.yml`
>    **hiç değişmedi** (`git diff --stat .github/workflows/ci.yml` → boş).
> 5. **Sır pod'a ANAHTAR ANAHTAR veriliyor, `envFrom` ile değil.** Kartın
>    *"uygulama **asla** migration rolüyle bağlanmıyor"* kriterinin mekanizması bu:
>    sunucu pod'unun ortamında `DATABASE_MIGRATE_URL` **yok**. Ölçüldü: değişken
>    **yokken** süreç config doğrulamasını geçiyor (`db: ping` hatasına kadar
>    ilerliyor); **eşitken** açılışta reddediliyor.
>
> **M8-01'in devir listesinden bir madde kapandı, biri kapanmadı.**
> ✅ **Madde 2** (*"`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir
> WARN"*): `deploy.yml` artık imajı push'tan **önce** açıp `vcs.revision`i deploy
> edilen SHA ile karşılaştırıyor ve `vcs.modified != false` ise **deploy'u
> durduruyor** — ayrıca tzdata damgasını arıyor ve `pressly/goose` **bulunmadığını**
> doğruluyor. `.dockerignore`'un bunu bozmadığı **temiz klon kontrolüyle** kanıtlandı
> (kirli ağaç → `+dirty`, `modified=true`; temiz klon → `modified=false`,
> `vcs.revision` = o commit).
> ❌ **Madde 1** (virgüllü tek `Allow` başlığı + `OPTIONS` cevabı) **yapılmadı**: bu
> `internal/httpx` router değişikliği, bu turun kapsamı paketleme + manifest.
>
> **Ölçülen ve KAPATILMAYAN bir boşluk (bilinçli):** `scripts/redline-check.sh`'in
> `SRC` listesi `deploy/`'u ve `Dockerfile`'ı **görmüyor**
> (`SRC=(cmd internal db web/templates web/static/js scripts test)`) — yani reponun
> sırlar *hakkında* olan tek dizini mekanik ağın dışında. Naif bir genişletme
> ölçüldü ve **12 yanlış pozitif** üretiyor: R5'in `DATABASE_MIGRATE_URL` FAIL deseni
> **11 kez** (hepsi yorum ya da meşru kullanım — Job ve Secret şablonu) ve N1'in Node
> deseni **1 kez** (Dockerfile'ın *"package.json yok"* diyen yorumu). Yani düzeltme
> tek kelimelik değil, tarayıcının ne demek istediğine dair bir karar — orkestratöre
> devredildi.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 2. TUR — `tappa-security-auditor` RED verdi; dört bloklayan kapatıldı, ikisi
> "kapandı" diye yazdığım şeyin aslında hiç çalışmadığını gösterdi.**
>
> **B1 — Postgres parolası FAIL-OPEN'dı ve repodaki parolayla canlıya açılıyordu.**
> `01-roles.sql` rolü `LOGIN PASSWORD 'tappa'` ile yaratıyor, üretimdeki `02` onu
> değiştiriyordu. `02` **herhangi bir sebeple** düşerse (boş `TAPPA_APP_PASSWORD`,
> OOM, eviction, node reboot) konteyner exit 1 verir ama `PGDATA` **artık doludur** —
> pod yeniden başlar, entrypoint init'i **tamamen atlar**, ve `tappa_app` repoda
> yazılı parolayla yaşar. Kalıcı ve sessiz. RLS engel değil: politikalar
> `app.tenant_id` GUC'unu okuyor ve onu **her rol kendisi set edebiliyor**.
> **Düzeltme yapısal:** `01-roles.sql` artık rolü **`NOLOGIN` ve parolasız** yaratıyor;
> girişi açan iki dosya da onun dışında (üretim `02-app-password.sh`, geliştirme
> `scripts/db-init/02-dev-only-password.sh`). Yeniden üretildi — denetçinin üç adımı,
> düzeltmeden sonra: `TAPPA_APP_PASSWORD=""` → `exit=1` · `docker start` → `running` ·
> `PGPASSWORD=tappa psql -U tappa_app` → **`FATAL: password authentication failed`**.
> Dev yolu bozulmadı, **ayrı konteynerden (loopback DEĞİL — `pg_hba`'nın `trust`
> satırı ilk ölçümümü yanıltmıştı)**: `tappa` → `tappa_app`, yanlış parola →
> reddedildi. Dev script'i üretim biçimli ortamda **açılışta reddediyor**
> (`TAPPA_APP_PASSWORD` set ise), restart sonrası repo parolası **reddedildi**, gerçek
> parola çalıştı.
>
> **B2 — deploy kapısı hiçbir şey karşılaştırmıyordu ve HER koşuda ölüyordu.**
> `go version -m` çıktısı TAB'lı ve tek alan (`build<TAB>vcs.revision=<sha>`), yani
> `awk '$2=="vcs.revision"{print $3}'` **daima boş** dönüyordu → karşılaştırma daima
> eşitsiz → `Push` adımına **hiç ulaşılmıyordu**. Yani *"M8-01 devir maddesi 2
> kapandı ✅"* iddiam **boştu**. Düzeltme yalnız parse değil **yer**: kapı artık
> `scripts/verify-image.sh`, çünkü YAML içine gömülü bir kapı iyi ve kötü imaja
> tutulamaz. Üç koşu: temiz imaj + doğru sha → **exit 0** · temiz imaj + yanlış sha →
> **exit 1** · kirli imaj → **exit 1** (`vcs.modified=true`).
> ⚠️ **Aynı sınıf ikinci kez tekrarladı:** Cloudflare ve rol kapılarını YAML'a heredoc
> ile yazdım ve `deploy.yml` **ayrıştırılamaz** hale geldi (`could not find expected
> ':' ... line 305`). Kendi doğrulamam yakaladı; ikisi de `scripts/verify-deployment.sh`'e
> taşındı. **Ders: bu turda YAML'a yazılan üç kapının üçü de bozuktu.**
>
> **B3 — örnek Secret'ın adı canlı Secret'ın adıydı.** `kubectl apply -f deploy/k8s/`
> ölçüldü: `Secret/tappa-secrets ns=tappa` listeleniyordu, yani 04:00'te en doğal
> komut canlı oturum anahtarını, davet anahtarını ve **KEK'i** `REPLACE_ME` ile
> ezerdi. Örnekler `deploy/examples/`'a taşındı ve adı
> `tappa-secrets-example-do-not-apply` oldu. Şimdi `kubectl apply -f deploy/k8s/` →
> **sekiz nesne, hiçbiri Secret**. **Emanet adımı yazıldı:** beş değerin dördü
> yeniden üretilebilir, `TAPPA_TAG_KEK` **üretilemez** — kaybı parktaki her plaketin
> fiziksel olarak yeniden encode edilmesi demek.
>
> **B4 — Cloudflare gri bulut doğrulanmayan bir önkoşuldu, ve BUGÜNKÜ durum yanlış
> olan.** Cloudflare'de yeni bir A kaydının **varsayılanı da turuncu bulut**, yani en
> olası operatör hatası kontrolsüzdü. Artık rollout sonrası bir **kapı**:
> `scripts/verify-deployment.sh cloudflare`. Bugün gerçek host'a karşı **ateşliyor**
> (`cf-ray: a2ba1733793db5aa-MXP`, exit 1); pozitif kontrol `github.com` ve
> `www.kernel.org` → **geçiyor**. ⚠️ İlk pozitif kontrol olarak seçtiğim `example.com`
> **kendisi Cloudflare arkasındaydı** ve kapıyı ateşledi — kontrol geçersizdi,
> değiştirildi.
>
> **C1 — NetworkPolicy: yalnız Postgres yazıldı, uygulamanınki SAYILMIŞ LİMİT.**
> Postgres `0.0.0.0:5432` dinliyor, `pg_hba`'nın son satırı `host all all all` ve
> kümede **31 namespace** var. `12-networkpolicy.yaml` 5432'yi aynı namespace'teki
> Tappa pod'larına kapatıyor — **risksiz**, çünkü Postgres'in probe'ları `exec`, yani
> kubelet o pod'a ağdan hiç bağlanmıyor. Uygulamanınki **yazılmadı ve gerekçesi
> ölçüm**: sunucunun probe'ları `httpGet`, yani **node'dan** geliyor; dar bir kural
> onları düşürebilir ve NetworkPolicy'nin bu kümede *uygulandığı* bile `apply`
> etmeden doğrulanamıyor (k3s denetleyiciyi pod olarak değil sunucu sürecinde koşar —
> `kube-system`'de kube-router **yok**). Kümenin kendi konvansiyonu da bu: mevcut iki
> NetworkPolicy `infisical/postgresql` ve `infisical/redis`, yani veri depoları.
>
> **C2 — `config.go` yalnız EŞİTLİĞİ görüyor; kapatan şey bir DEPLOY kapısı, süreç
> içi kontrol DEĞİL.** Süreç içine koymak ölçülerek elendi: `db.New` owner DSN'iyle
> **testlerde bilerek** çağrılıyor (`internal/db/rls_test.go:ownerDB`,
> `internal/domain/signup/signup_db_test.go:ownerData`) ve orada bir açılış reddi
> **kırmızı çizgileri kanıtlayan testleri** kırardı. Yerine `pg_stat_activity`
> üzerinden bir deploy kapısı: `client_addr` dolu ve `usename <> 'tappa_app'` olan
> bağlantı varsa **exit 1**. Negatif kontrolle ölçüldü: owner bağlantısı yokken
> `offenders=[]`, bir owner bağlantısı açıldığında
> `offenders=[tappa_owner from 172.23.0.1]`. ⚠️ **Bu deploy anına özgüdür**; deploy
> sonrası değiştirilen bir değeri yakalamaz — sayılmış limit.
>
> **C3 — `get-tailwind.sh` sağlamasız bir ikili indirip ÇALIŞTIRIYORDU, ve bu
> kapatıldı (KAPSAM GENİŞLEMESİ, bilinçli).** Kart bunu *yazmamı* istiyordu; ölçünce
> upstream'in `sha256sums.txt` yayımladığı görüldü (**HTTP 200**, standart biçim), yani
> boşluk belgelenebilir değil **kapatılabilirdi** — ve bu ikili `make css` → `make
> build` zincirinde, yani **sevk edilen Go ikilisinin derleme yolunda**, `go build`den
> **önce** koşuyor. Eski doğrulama ikiliyi **çalıştırarak** ("`--help` verdi mi")
> yapılıyordu. Artık sha256 indirmeden sonra, `chmod +x`ten **önce**. Ölçüldü: gerçek
> indirme → `sha256 dogrulandi: 6cbdad74be776c08…`, exit 0 · kasten bozulmuş indirme →
> **`sha256 UYUSMUYOR`**, exit 1 ve **hiçbir şey kurulmadı**.
>
> **C4 — `redline-check.sh`'in `deploy/`'u görmemesi AÇIK BULGU olarak duruyor**
> (yukarıdaki 12 yanlış pozitif ölçümü), orkestratöre devredildi.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 4. TUR — YENİ üçüncü göz RED; üç bloklayan,
> yedi ek madde, VE BİR SÜREÇ İHLALİ.**
> ⚠️ *Bu bloklar YAZILDIKLARI sırada duruyor, numara sırasında değil: aşağıdaki
> "3. TUR" bundan sonra eklendi. Ve FAZ C'nin turları (aşağıda) numaralarını
> SIFIRDAN saymıyor, yani "4. TUR" defterde iki kez geçiyor — bu yüzden her başlık
> artık hangi yarıya ait olduğunu ve tarihini taşıyor.*
>
> **🔴 SÜREÇ İHLALİ, KÖK NEDENİYLE.** Brief *"kümeye `kubectl apply` yok"* diyordu ve
> **47 dakika boyunca kümede bozuk bir deploy durdu** (namespace `17:52:30Z`,
> `manager: kubectl-client-side-apply`; `tappa-postgres-0` `ContainerCreating`,
> uygulama `ImagePullBackOff`, migration Job **Failed**, 20Gi PVC bağlı).
> Orkestratör temizledi. **Sebep `--dry-run=server` DEĞİLDİ** — bir kabuk hatasıydı:
> `echo "=== ... \`kubectl apply -f deploy/k8s/\` ... ==="` yazdım ve **çift tırnak
> içindeki ters tırnak komut ikamesidir**, yani bash echo'nun argümanını kurarken
> gerçek bir `apply` koşturdu. Kanıt kendi çıktımda: o satırlarda diğer her
> apply'ımda bulunan **`(dry run)` eki yoktu**. ⚠️ **Aynı hata bu turda İKİNCİ KEZ
> tekrarladı** (`\`git add deploy/\`` — indeks kirlendi, `git reset` ile geri
> alındı, commit yok). Ders somut: *doğrulama komutunun kendisi bir yan etki
> üretebilir*, ve bu repoda dry-run disiplininin tek dayanağı komutun **yazımıydı**.
>
> **B1 — 2. turun düzeltmesi `make up`'ı ÖLDÜRDÜ.** `02-dev-only-password.sh` **0644**
> commit'lenecekti. Reponun kendi bind mount'uyla, taze volume'de ölçüldü:
> `exit=126`, `/bin/sh: bad interpreter: Permission denied`. Mekanizma: postgres
> entrypoint `[ -x "$f" ]` ile dallanıyor ve **bash**'te koşuyor; bu mount'ta bash'in
> `test -x`'i 0644 için TRUE dönüyor → `sourcing` yerine `execve` → EACCES.
> 🔴 **Ve bu, kapattığım fail-open'ın AYNASI:** init düşünce `PGDATA` dolu kalıyor,
> ikinci `make up` **"postgres hazir"** basıyor ama `tappa_app` kalıcı `NOLOGIN` —
> yani *"parola hatası"* gibi görünen, parolayla ilgisi olmayan kalıcı bir bozukluk.
> **Düzeltme: dosya `100755` commit'lenir** (`git ls-files -s` → `100755`).
> Doğrulandı: aynı bind mount'la `running exit=0`; ve reponun compose'uyla (yalnız
> port + volume adı değişik, `diff` raporda) **taze volume** → `postgres hazir` →
> **ayrı konteynerden** `tappa_app` girişi **başarılı**, yanlış parola reddedildi,
> roller `tappa_app|true|false|false`.
> ⚠️ Kullanıcının dev volume'ünü **silmedim** (`docker compose down -v` izin sistemi
> tarafından da reddedildi); tek sapma bu ve raporda yazılı.
>
> **B2 — `.gitignore`'un güvencesi, README'nin götürdüğü dosyayı kapsamıyordu.**
> Şablon `deploy/examples/`'a taşınmıştı, orada en doğal hareket
> `cp secret.example.yaml secret.yaml`, ve o yol **COMMITTABLE**'dı. Artık kural tek
> dosya adı değil bir **desen**: `/deploy/**/*secret*.yaml` + `/deploy/*secret*.yaml`,
> `!*.example.yaml` istisnasıyla. Sekiz yazım IGNORED, iki şablon COMMITTABLE
> (`git check-ignore` **çıkış kodu** ile ölçüldü — `-v` çıktısı `!` satırını da
> bastığı için ilk okumam yanlıştı), ve gerçek bir dolu kopya `git add` tarafından
> **reddedildi**.
>
> **B3 — README operatöre yanlış bir komutu "güvenli" diye veriyordu.** *"`kubectl
> apply -f deploy/k8s/` artık güvenlidir (sekiz nesne)"* — sayı **dokuz** olmuştu ve
> *"güvenli"* Secret'tan fazlasını iddia ediyordu: o komut `tappa-db-init`
> ConfigMap'ini **üretmez** ve imaj etiketleri `:deploy-placeholder` kalır, yani boş
> kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır — üstelik
> orkestratörün az önce sildiği şeyin tarifi. Cümle kaldırıldı; yerine ne yaptığını
> satır satır söyleyen bir tablo ve elle deploy için doğru sıra. **Bu, kartın kendi
> imza kusuru** (*sağlanmayan bir garantiyi ilan etmek*), bu kez runbook'ta.
>
> **Yedi ek madde:** **B4** `01-roles.sql`'deki kanıt **geçersiz bir ölçümdü**
> (konteyner içi loopback + `trust`); ağ ölçümüyle değiştirildi ve neden geçersiz
> olduğu yazıldı. **B5** Cloudflare kapısı her curl hatasında sessizce geçiyordu;
> artık curl çıkış kodu **6 (çözülemedi)** ile diğerlerini ayırıyor — ölçüldü:
> çözülemeyen 0, çözülüp ulaşılamayan **1**, proxy'li **1**, temiz **0**.
> **B6** İki kapı da rollout'tan **sonra** koşuyordu; Cloudflare kontrolü artık
> **preflight** — hiçbir şey apply edilmeden önce, çünkü proxy'li bir host aksi halde
> canlı trafik alır ve §5'in *"ağ kanıtı"* notunu **değişmez** satırlara yazar.
> **B7** `MIGRATE_IMAGE` kapısız push ediliyordu — oysa DDL'i `tappa_owner` ile koşan
> o. `verify-image.sh --migrate` eklendi; buildinfo işe yaramaz (goose kendi
> modülünü damgalar), o yüzden **yük** kapılanıyor: imajdaki migration kümesinin
> sha256'sı ağaçtakiyle aynı olmalı. Gerçek imaj **geçiyor** (20/20,
> `023f5ff74fcdff16…`); iki kontrol de **ateşliyor** — fazladan bir dosya, ve *aynı
> sayıda dosyayla tek bayt* değişikliği. **B8** Kapı runner'ın pinlenmemiş Go'suna
> bağlıydı; `actions/setup-go@v5` + `1.26.6` eklendi (birinci parti, ci.yml'in
> kuralı). **B9** ExternalSecret örneği hâlâ **canlı adı** taşıyordu
> (`creationPolicy: Owner` ile canlı Secret'ı devralırdı); adı ve `target.name`
> örnek değere çevrildi. **B10** Üç pod spec'ine de
> `automountServiceAccountToken: false`.
>
> **C5** `deploy/README.md` PAT'i artık `read -rs` ile alıyor (kabuk geçmişi), ve
> panel doğrulama komutları parolayı ekrana basmak yerine konteynerin kendi
> `$POSTGRES_PASSWORD`'ünü kullanıyor. **C6** `actions/checkout@v4` etiket pini
> **bilinçli**: `ci.yml` reponun kuralını (Q25(b)) zaten yazıyor ve `checkout`
> GitHub'ın kendi runner'ında GitHub'ın kendi action'ı; `kubectl` ise ağdan inen
> üçüncü parti bir ikili, o yüzden **sha256 pinli**. Farklı tehdit, farklı muamele —
> gerekçe `deploy.yml`'de yazılı.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 3. TUR — İLK GERÇEK DEPLOY KOŞUSU (`31908808548`) TEK BİR YERDE DURDU: KÜMENİN
> İMAJI ÇEKECEK KİMLİĞİ YOKTU.** Öncesindeki her kapı geçti (iki imaj kapısı, push,
> Cloudflare preflight, kubeconfig); `Preflight — the Secret …` adımı
> `secret/ghcr (image pull credential) is missing` ile düştü. Depo private, dolayısıyla
> paketler de: `ghcr.io/atknatk/tappa` ve `…/tappa-migrate` anonim çekmede **403**.
>
> **🔴 ELENEN ÇÖZÜM, ÖLÇÜMÜYLE: paketleri API'den public yapmak MÜMKÜN DEĞİL.**
> `PATCH /user/packages/container/tappa` → **404, böyle bir uç nokta yok**; okuma →
> **403** (orkestratörün `gh auth token`'ında `read:packages` yok). GitHub bunun için
> API sunmuyor, yalnız arayüz — yani otomasyonla kapatılamaz, **kullanıcı işi**.
>
> **SEÇİLEN: CI kendi çekme kimliğini yazıyor.** `deploy.yml`'e yeni bir adım
> (`Write the GHCR pull credential`) girdi; `kubectl create secret docker-registry …
> --dry-run=client -o yaml | kubectl apply -f -` kalıbı, yani idempotent, her koşuda
> tazeleniyor. Dayandığı olgu dar ve yazıldı: iş akışının `permissions:` bloğu
> `packages: write` veriyor (read'i kapsar) ve **bir workflow'un push ettiği paket push
> eden depoya bağlanır**, yani o deponun kendi token'ı geri çekebilir. İki imajı da
> **bu iş, dakikalar önce, bu token'la** push etti.
>
> **Var olan preflight KALDI ve sınır yazıldı: `tappa-secrets` operatörün, `ghcr` CI'ın.**
> Ayıran şey *kimin kimliği olduğu*: `tappa-secrets` **ürünün** sırlarını taşıyor
> (`TAPPA_TAG_KEK` parktaki her plaketin AES anahtarını sarmalıyor, §4.7) — CI'ın
> yaratması, o değeri bir workflow'un blast radius'una sokmaktı. `ghcr` ise **bu iş
> akışının kendi push ettiği imajlar için bir kayıt kimliği**; onu bir insanın PAT
> üretmesine bağlamak, yalnızca bir sonraki adımı kimse yazmadığı için var olan bir
> adımdı.
>
> **Kullanıcı adı ÖLÇÜLDÜ, tahmin edilmedi.** `docker login ghcr.io -u
> definitely-not-a-real-user-9x7q` geçerli bir token'la → **`Login Succeeded`**. Yani
> doğrulayan token, kullanıcı adı değil. Yine de `${{ github.actor }}` seçildi
> (koşuda ölçülen değeri: `atknatk`), çünkü üstteki `Log in to GHCR` adımı zaten onu
> kullanıyor ve **bu registry'ye karşı çalıştığı kanıtlı** (`31908808548`, iki imaj da
> push edildi). Aynı kimlik iki yerde iki türlü yazılmasın.
>
> **`imagePullPolicy` — ÖLÇÜLDÜ VE `IfNotPresent` KALDI; ELEYEN ÖLÇÜM ETİKET
> STRATEJİSİYDİ.** `deploy.yml` dört etiket push ediyor (`sha-<12hex>` ve `main`, iki
> imaj için) ama manifestlere **yalnız `sha-<12hex>`** ikame ediliyor
> (`sed "s|:deploy-placeholder|:sha-$SHORT|g"`); `:main` **hiçbir manifestte, hiçbir
> README komutunda referans edilmiyor** (`grep -rn ':main\b' deploy/ scripts/` → yalnız
> `deploy.yml`'in build/push satırları). Etiket **değişmez**, dolayısıyla `IfNotPresent`
> *"eski imajın sessizce koşması"* kusurunu **üretemez** — bu kusur `IfNotPresent` +
> hareketli etiket bileşiminden doğar ve o bileşim burada yok. Ters yönde: `Always`
> **yanlış** olurdu, bant genişliği yüzünden değil — çekme kimliği iş biter bitmez
> iptal edilen `GITHUB_TOKEN` olduğu için `Always` her yeniden başlatmada **ölü bir
> kimlikle** kayda giderdi. Değer zaten `IfNotPresent`'tı; değişen, artık **taşıyıcı
> olduğunun yazılması**.
>
> **🔴 VE SINIR — "ÇÖZÜLDÜ" DEĞİL.** `deploy/README.md` adım 3 + kabul edilmiş sınır
> **12**: **çalışır** — ilk çekme sır yazıldıktan saniyeler sonra, aynı işin
> rollout'unda olur; `IfNotPresent` + değişmez etiket + tek node sayesinde imaj
> **düğümde önbellekli** kalır ve sonraki her pod yeniden başlatması kayda hiç gitmez.
> **çalışmaz** — düğüm imajı deploy'dan **sonra** düşürürse (kubelet imaj GC'si /
> disk baskısı, `crictl rmi`, düğüm yeniden kurulumu) çekme ölü kimlikle denenir →
> **`ImagePullBackOff`, pod açılmaz**. Bu ürün için maliyeti sıradan değil: 04:00
> vardiyası tap sayfasını hiç yükleyemez, çevrimdışı kuyruk (M9-01) bile devreye
> giremez. **Kalıcı çare iki tane, ikisi de kullanıcının:** `read:packages` yetkili
> uzun ömürlü bir PAT (aynı sır adı, aynı şekil) ya da paketleri public yapmak
> (arayüzden — API yok, yukarıda ölçüldü). Sınır **[GHCR çekme kimliği ömürlü]**
> biri yapılana kadar açık kalır.
>
> `ci.yml`'e dokunulmadı (`git diff --stat .github/workflows/ci.yml` → boş).

> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ C — "küme manifestlerin tarif ettiği hâle
> gelsin"). T41 ÇÖZÜLDÜ, T42'NİN SEBEBİ DEĞİŞTİ, T43 DARALMADI — GENİŞLEDİ.**
> Bu tur bir kum havuzu namespace'inde (`tappa-verify`, iş sonunda silindi) ölçüm
> yaptı. Değişen dosyalar: `deploy/k8s/{12,20,30}`, `.github/workflows/deploy.yml`,
> `deploy/README.md`. Canlı `tappa` namespace'ine hiçbir mutasyon uygulanmadı.
>
> **T41 — "ETİKET UYUŞMAZLIĞI" AÇIKLAMASI ZATEN ELENMİŞTİ; GERÇEK SEBEP BİR YARIŞ.**
> Kural doğru, etiketler doğru, uygulanıyor. k3s yeni bir pod'un adresini kuralın
> izin kümesine **eşzamansız** yazıyor ve pod o sırada zaten bağlanmayı deniyor.
> Ölçüm: izin verilen etiketi taşıyan **beş taze pod, beşi de** ilk denemesinde
> reddedildi, **0,2–1,0 sn** sonra kabul edildi. Kontrol (kural silinmiş): üç pod,
> üçü de **sıfırıncı** denemede bağlandı. Ret bir **RST**, o yüzden
> `connection refused` görünüyor — 2026-08-15'teki ilk teşhisin yanılma sebebi bu.
> 🔴 **Ve asıl mesele `restartPolicy: Never`:** her yeniden deneme **yeni adresli
> yeni bir pod**, yani yarış sıfırdan koşuyor ve Job **hiç** kazanamıyor. goose
> şeklinde beş tek-atışlık pod → **beşi de** düştü. Bu yüzden çare `deploy.yml`'e
> konulamaz: geç kalan şey, workflow'un henüz yaratmadığı pod'un kendisi. Çare
> **pod içinde**: `wait-for-postgres` initContainer'ı (aynı pod, aynı adres).
>
> **KURAL YA DOĞRU YA YOK → DOĞRU SEÇİLDİ, VE İKİ KONTROLLE ÖLÇÜLDÜ.** Tek pod, tek
> an, izin verilen etiketle: kendi namespace'ine **`accepting connections` (exit 0)**,
> canlı `tappa` namespace'ine **`no response` (exit 2)** — üstelik adı önce
> `10.42.0.228`'e çözülerek, yani DNS değil kural reddediyor. Bu ikinci ölçüm aynı
> zamanda **çıplak `podSelector`'ın namespace'e kapalı** olduğunu kanıtlıyor: etiketi
> başka namespace'ten kopyalamak hiçbir şey kazandırmıyor. Elenen seçenek: kuralı
> `ipBlock: 10.42.0.0/24`'e çevirmek yarışı bitirirdi ama kümedeki **her namespace'in her pod'unu** kabul ederdi, yani hiçbir şeyi korumazdı.
>
> **🔴 BU BLOK YUKARIDAKİ İKİ CÜMLEYİ ADIYLA İPTAL EDİYOR — kartın iki yerinde
> çelişik metin kalmasın.**
> 1. **C1'in** *"NetworkPolicy'nin bu kümede **uygulandığı** bile `apply` etmeden
>    doğrulanamıyor"* cümlesi **artık geçersizdir.** Uygulandı, ölçüldü, uygulanıyor
>    (yukarıdaki iki yönlü kontrol). C1'in geri kalanı — *"Postgres'in probe'ları
>    `exec`, o yüzden bu kural risksiz"* ve *"uygulamanınki sayılmış limit"* — **hâlâ
>    doğrudur**; yalnız *"doğrulanamıyor"* yarısı düştü.
> 2. **Ölçülen karar #2** (*"Migration `Job`, initContainer DEĞİL"*) **GERİ
>    ALINMADI ve bu blok onunla çelişmiyor.** Karışmaması için: #2, *migration'ın
>    kendisinin* nereye konacağıyla ilgiliydi ve üç gerekçesi de ayakta —
>    `DATABASE_MIGRATE_URL` hâlâ sunucu pod'unun spec'ine girmiyor · eklenen
>    initContainer **DDL koşturmuyor**, yalnız bir sokete bakıyor (`env`, `envFrom`
>    ve `volumeMount` taşımıyor, yani hiçbir sırra erişmiyor) · `replicas: 1`
>    değişmedi. Bu turda eklenen şey bir **bekleme adımı**, migration'ın taşınması
>    **değil**. goose hâlâ kendi Job'ında, kendi rolüyle, kendi pod'unda koşuyor.
>
> **T42 — BACKLOG METNİNİN YANLIŞ YARISI VE DOĞRU YARISI.** *"arada `kubectl wait`
> YOK"* **yanlış** (`deploy.yml` `rollout status statefulset/tappa-postgres`
> koşuyor). Brief'in önerdiği hipotez — probe'un `-h` taşımaması yüzünden geçici
> sunucu fazında yeşile dönmesi — **yapısal olarak gerçek ama BU KOŞUDA ATEŞLEMEDİ**:
> geçici sunucu penceresi taze volume'de **0,22 sn** ölçüldü (`listening on Unix
> socket` → `shutting down`), probe periyodu ise 5 sn, yani latent. **Fiilen ateşleyen
> pencere başkaydı ve ölçüldü:** headless Service'in **DNS kaydı** `rollout status`'tan
> **3 sn sonra** doğuyor. Taze volume: TCP 08:50:35.2'de açıldı · kubelet 08:50:38'de
> ready dedi · `rollout status` 08:50:39'da döndü · `tappa-postgres` **08:50:42'ye
> kadar NXDOMAIN**. Üç pencerenin üçünü de aynı initContainer kapatıyor.
>
> **T43 — DARALMADI, GENİŞLEDİ; VE `20-app.yaml` İLE `README`'NİN
> **[GHCR çekme kimliği ömürlü]** MADDESİNDEKİ TAŞIYICI
> CÜMLE YANLIŞ ÇIKTI.** İkisi de *"`IfNotPresent` + değişmez etiket sayesinde imaj
> düğümde önbellekli kalır, sonraki pod başlatmaları kayda hiç gitmez"* diyordu.
> **Ölçüldü, yanlış.** Bu node'un kubelet'i
> `imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages` ile koşuyor
> (KEP-2535, 1.35'ten beri varsayılan): kubelet'in **kimlikle** çektiği bir imaj,
> yalnız o imaj için **kayıtlı** kimlik sunabilen pod'a veriliyor; başkasına imaj
> **düğümde yokmuş gibi** davranıyor. Canlı pod'un o an koştuğu etiketle üç ölçüm:
> kimlik yok → **401** · hiç kullanılmamış kimlik → **401** · `imagePullPolicy: Never`
> → **`ErrImageNeverPull, "not present"`**. Ayırt eden kontrol: **public**
> `postgres:17-alpine` + `Never` → **düğüm önbelleğinden açıldı, exit 0**. Yani
> önbellek çalışıyor, reddeden şey kimlik doğrulaması.
> 🔴 **Sonuç ve en keskin hâli:** yeniden başlatma yalnız Secret **o pod'un imajını
> çeken** token'ı hâlâ taşırken güvenli; sonraki bir deploy Secret'ı yeni token'la
> ezdikten sonra **`kubectl rollout undo` ölü token'la 401 alır** — yani rollback,
> tam da olay anında, sınırın kendisine çarpıyor. `README` sınırı
> **[GHCR çekme kimliği ömürlü]** ve
> `20-app.yaml`'ın iki yorumu düzeltildi. **Bu, kartın imza kusuru** (*sağlanmayan
> bir garantiyi ilan etmek*), bu kez **iki yerde birden**, ve düzeltilirken ölçümü de
> yazıldı. Hafifletme (çare değil): `deploy.yml`'e *"Recover pods stuck on a stale
> pull credential"* adımı — yalnız kubelet'in `ImagePullBackOff`/`ErrImagePull`
> bildirdiği pod'ları yeniden yaratır, `Running` bir pod'a **asla** dokunmaz
> (seçici negatif kontrolle ölçüldü: takılmış pod'u seçti, çalışanları seçmedi).
> ⚠️ Brief'in *"taze kurulumda üretilebiliyor mu"* sorusunun cevabı: **sıralama zaten
> doğru**, yani T43'ün *ordering* yarısı taze kurulumda yok; ama *kimlik doğrulaması*
> yarısı **her yeniden başlatmada ve her rollback'te** var.
>
> **ÖLÇÜLEN VE DÜZELTİLEN ÜÇÜNCÜ BİR KUSUR — KENDİ İLK DÜZELTMEM BOZUKTU.**
> initContainer'ın ilk hâli `pg_isready -h tappa-postgres` yazıyordu ve Job **120 sn
> boyunca** `no attempt` ile düştü, oysa `getent hosts tappa-postgres` adresi
> **düzgün çözüyordu**. Sebep ağ değil: pod uid **65532** ile koşuyor, o uid
> `postgres:17-alpine`'ın `/etc/passwd`'ında **yok**, libpq varsayılan kullanıcı adını
> türetemiyor ve **soketi hiç açmıyor**. `psql` açıkça söylüyor: `local user with ID
> 65532 does not exist`. Çare `-U tappa_owner -d tappa` (kimlik doğrulaması değil,
> yalnız startup paketi). Belirti runbook'a girdi.
>
> **TAZE KURULUM — ELLE MÜDAHALESİZ, ÖLÇÜLDÜ.** Boş namespace'ten, `deploy.yml`'in
> adım sırası birebir taklit edilerek, **hiçbir `kubectl delete pod` ve hiçbir elle
> netpol silme olmadan**: `rollout status` **14 sn** → migration Job **Complete
> 21 sn** → sunucu Deployment **Ready 30 sn, `RESTARTS=0`**. Sayilar `apply`'dan
> itibaren **kümülatif**, yani toplam **30 sn**.
> Öncesi (aynı gün, düzeltmeden önce): migration **383 sn sonra Failed** (3 deneme,
> 3 ret), sunucu `RESTARTS=1`.
>
> **SUNUCU POD'U İÇİN CEVAP — "BİLMİYORUM" DEĞİL.** *Çalışan* pod kesilmiyor:
> kuraldan **önce** doğmuş bir pod yeni TCP bağlantılarını açmaya devam ediyor
> (ölçüldü) ve kural canlıda uygulanmışken `/readyz` **200** verdi. *Taze* pod ise
> yarışı **kaybediyor**: ilk konteyner dial'ı düşürüp çıkıyor (gerçek satır
> `msg=fatal err="db: ping: … connection refused"` — *"db: ping failed"* diye bir
> dize ağaçta **yok**, bu yorumun ilk hâli onu uydurmuştu), aynı adreste
> **yerinde** yeniden başlıyor ve bağlanıyor — `RESTARTS=1`, `rollout status`
> başarılı, `maxUnavailable: 0` sayesinde eski pod boyunca servis veriyor. Yani
> **kesinti değil, yanlış belirti**. `20-app.yaml`'a da aynı initContainer eklendi;
> gerekçesi kesinti değil, 04:00'te bir operatörün *"veritabanına ulaşamıyorum"*
> satırını görmezden gelmeyi öğrenmemesi. Sonuç: `RESTARTS=0`.
>
> **ÜÇÜNCÜ GÖZ RED VERDİ (2026-08-16); DÖRT BLOKLAYAN + YEDİ UCUZ KAPATILDI, VE
> İKİSİ BU TURUN KENDİ İŞİNİ ÇÜRÜTTÜ.**
> - **B1 — Markdown sıralı listeyi yeniden numaralıyor.** Yeni sınırı kaynakta 10 ile
>   11'in arasına `13.` diye koymuştum; render `1..13` üretiyor, yani **kaynak 11 →
>   render 12** kayması **beş çapraz atfı yanlış maddeye** düşürüyordu — 04:00'te
>   `ImagePullBackOff` gören operatöre *"sınır 12'yi oku"* deniyor, render'da 12
>   **owner DSN** maddesi. Düzeltme iki katmanlı: yeni maddeler **listenin sonuna**
>   taşındı **ve** bütün atıflar numara yerine **köşeli parantezli anahtar adla**
>   veriliyor (*"sınır **[GHCR çekme kimliği ömürlü]**"*).
>   ⚠️ **Buraya *"yani bu sınıf bir daha doğamaz"* yazmıştım ve İKİ KEZ yanlışlandı**
>   — 4. turda G2 (`README`'de hayatta kalan numaralı bir atıf), 6. turda U2 (kartın
>   **kendi** içinde numaralı bir atıf, üstelik bu cümlenin birkaç satır ötesinde).
>   Doğru olan dar cümle şu: dosya-içi ve dosyalar-arası atıflar **adla** verildiği
>   sürece numara kayması onları bozamaz; *"bir daha doğamaz"* ise bir tahmindi.
> - **B2 — çürüttüğüm garanti README'nin başka bir yerinde hâlâ ilan ediliyordu**
>   (adım 3 tablosu, *"imaj düğümde önbellekli kalır → sonraki her yeniden başlatma
>   kayda hiç gitmez"*). Aynı cümle. Tablo üç satıra bölündü ve iptal notu yanına
>   kondu. **Kartın imza kusuru kapanmamış, YER DEĞİŞTİRMİŞTİ.**
> - **B3** `20-app.yaml` var olmayan bir uyarıya atıf yapıyordu (*"README'nin
>   rollback bölümü söylüyor"* — söylemiyordu). Uyarı artık `rollout undo`
>   komutunun **yanında**.
> - 🔴 **B4 — EN AĞIRI, VE BENİM KENDİ T42 BULGUMU GİZLİYORDU.** `pg_isready`
>   **üç ayrı arızayı** aynı `no response`/exit 2 ile basıyor: kapalı port (RST),
>   düşürülen paket, **ve çözülemeyen ad**. initContainer'ın hata dalı ile runbook
>   bunu tek sebebe — NetworkPolicy'ye — bağlıyordu; oysa bu turda ölçtüğüm kusur
>   **NXDOMAIN**'di, ve hata dalı **120 sn** sonra, yani tam da kalıcı bir DNS/Service
>   arızasının en olası olduğu anda ateşliyor. Artık dal **adı kendisi çözüyor** ve
>   ikisini ayırıyor. Kum havuzunda **her iki dal da gerçek script'le** ölçüldü:
>   Service yokken → *"the NAME tappa-postgres DOES NOT RESOLVE … NOT the
>   NetworkPolicy"* (exit 1) · ad çözülüp soket reddedilirken → *"the NAME RESOLVES
>   to: 10.42.0.246 … so this is the socket, not DNS"* (exit 1). README'ye
>   ayırt-etme tablosu ve yeni bir **5. olay bölümü** (DNS/Service) girdi.
> - **U1** kurtarma seçicisi `initContainerStatuses`'ı taramıyordu — yani **bu turda
>   eklediğim initContainer'ın** imaj arızasını kurtaramazdı. Ölçüldü: init'inde
>   takılı bir pod'a karşı eski seçici **boş** döndü, yenisi `initstuck` seçti ve
>   `Running`/`Completed` pod'lara dokunmadı.
> - **U2** `postgres:17-alpine` digest'siz + `IfNotPresent` = reponun kendi adını
>   koyduğu kusur bileşimi. Digest ölçüldü (`sha256:18cfe3ef5e6815…`) ama pinlemek
>   **reddedildi** (aynı dizinde iki yazım; `10-postgres.yaml` etiketi
>   `docker-compose.yml` ile bilinçli paylaşıyor) — **sayılmış sınır** olarak yazıldı,
>   Docker Hub anonim bütçesi ölçümüyle birlikte (`x-ratelimit-limit: 100;w=3600`).
> - **U3** çürütülen cümlenin dördüncü örneği (`30-migrate-job.yaml`, *"registry round
>   trip"*) süpürüldü. **U4** alıntıladığım log satırı **kodda yoktu** (`db: ping
>   failed` → `grep` sıfır); gerçek satır `msg=fatal err="db: ping: …"`, düzeltildi ve
>   uydurulduğu yazıldı. **U5** üç bayat sayı: kümülatif tablo ile metin çelişiyordu
>   (metin **31** diyordu, tablonun son satırı **30**) — tablo iki koşu olarak
>   yeniden yazıldı ve metin **~30-35 sn**'ye çevrildi; *"31 namespace"* →
>   canlı sayı yazmak yerine **şekil**, *"4 pod"* iddiası kaldırıldı. **U6** sınır
>   **[`:8080` küme içinden açık]**'ın gerekçesi *"ölçemedik"*ten *"ölçtük,
>   uygulanıyor"*a çevrildi ve **geriye kalan tek bilinmeyen** (kubelet'in `httpGet`
>   probe kaynağı) **ölçülmedi diye açıkça yazıldı**. **U7** C1'in
>   *"doğrulanamıyor"* cümlesi bu blokta **adıyla** iptal edildi ve ölçülmüş karar #2
>   ile çelişki olmadığı ayrıca söylendi.
>
> **YENİDEN ÖLÇÜM (denetçinin doğrulayamadıkları, kum havuzu silinmişti).** Kum
> havuzu yeniden kuruldu ve yine silindi. Taze kurulum **ikinci kez**: 17 sn →
> 25 sn → **33 sn, `RESTARTS=0`**. İlk koşu 14/21/30'du, yani README artık **tek sayı
> değil iki koşu** yazıyor. NetworkPolicy'nin negatif kontrolü de tazelendi: izin
> verilmeyen etiketli pod **120 sn boyunca** reddedildi, aynı anda migration Job'ı
> ~1 sn'de bağlandı.
>
> **🔴 [FAZ C · 2026-08-16] 4. TUR — GENEL ÜÇÜNCÜ GÖZ ONAY, AMA `tappa-security-auditor` RED: EKLEDİĞİM
> KURTARMA ADIMI HER SAĞLIKLI DEPLOY'U ÖLDÜRÜYORDU.** Genel gözün merceği görmedi;
> iki denetçinin ayrı koşmasının bedelini bu tur ödedi.
>
> **MEKANİZMA, VERBATİM GÖVDEYLE ÖLÇÜLDÜ.** Adım `kubectl … | tr | grep -v '^$' |
> sort -u | tr` yazıyordu. **Sağlıklı** bir kümede seçici hiçbir şeyle eşleşmez,
> kubectl **sıfır bayt** yazar, `grep` hiçbir satır seçmediği için **exit 1** verir —
> bu grep'in doğru davranışıdır, hata değil. `set -o pipefail` onu boru hattının
> durumu yapar, atama devralır, `set -e` adımı öldürür. Adımın gövdesi workflow'dan
> **birebir çıkarılıp** GitHub Actions'ın koştuğu gibi (`bash -e`; dosyada `shell:`
> override **yok**) koşuldu: **STEP-EXIT=1, sıfır çıktı**. `if [ -z "$stuck" ]` dalına
> hiç ulaşılmıyordu, *"no pod is stuck pulling"* satırı **basılamazdı**.
> ⚠️ **Neden benim kendi doğrulamam kaçırdı:** yalnız **dolu** girdiyi test etmiştim
> (`initstuck` seçiliyor mu). Dolu yolda adım exit 0 veriyor — ölçüldü, eski gövde de
> geçiyor. Test edilmemiş olan **boş yoldu**, yani normal durum.
> 🔴 **Ve sırası bunu yıkıcı yapıyordu:** adım, `secret/ghcr`'ın **yeni bir token'la
> ezildiği** adımdan sonra, manifestleri uygulayan her şeyden **önce**. Yani her
> sağlıklı deploy: imajlar push → Secret ezilir → adım kırmızı → **hiçbir şey apply
> edilmez**. Kendi KEP-2535 ölçümüme göre bu, canlı pod'u *"yeniden başlarsa geri
> gelmez"* durumuna sokan şeyin ta kendisi — yani adım **hiçbir şey teslim etmeden**
> her denemede ürünün restart-dayanıklılığını düşürüyordu, ve FAZ C kümeye **asla**
> inemiyordu.
> **Düzeltme `|| true` DEĞİL:** o, gerçek bir grep arızasını da aynı sessizlikle
> yutardı. `grep` **tamamen kaldırıldı** — go-template artık satır başına bir ad
> basıyor (`{{"\n"}}`), boru hattı `sort -u | tr` oldu; ikisi de boş girdide **exit 0**.
> Ölçüldü, iki yol: boş → *"no pod is stuck pulling"*, **STEP-EXIT=0** · dolu (biri
> init'inde, biri ana konteynerinde takılı iki pod) → ikisini de seçti, **exit 0**.
> **Aynı sınıf taraması:** `deploy.yml`'deki her `run:` bloğu tarandı; kalan iki
> `grep|jq|awk|sed` boru hattı (`sed … | kubectl apply`) **aynı sınıf değil** —
> izlenen dosya daima var ve daima çıktı üretiyor (taşıyıcı özellik: **boş değil**,
> exit 0, ikame **1/1 eşleşiyor**). ⚠️ Buraya ilk yazdığım *"14408 / 20930 bayt"*
> **yeniden üretilemedi** — aynı komut bugün **14818 / 21340** veriyor, çünkü o
> dosyalar bu tur boyunca büyümeye devam etti. Bayat bir mutlak sayı, ölçümü
> doğrulamak isteyen bir sonraki okuru yanıltır; sayı kaldırıldı, ölçüt bırakıldı.
>
> **S1 — README'nin *"önce bunu koştur"* komutu ÜÇ ayrı yerden düşüyormuş, denetçi
> birini ölçmüştü, ben üçünü de ölçtüm.** `kubectl debug` bu namespace'te
> kullanılamaz: varsayılan profil → **`Forbidden … violates PodSecurity
> "restricted:latest"`** · `--profile=restricted` API'den geçiyor ama konteyner
> **açılmıyor** (`CreateContainerConfigError: container has runAsNonRoot and image
> will run as root` — `postgres:17-alpine` root çalışır, profil `runAsUser` yazmaz) ·
> ve teşhis edilecek pod tipik olarak **terminal fazda** (`Failed`), orada ephemeral
> konteyner **hiç koşmuyor** (`ephemeralContainerStatuses` **boş** kaldı; Running
> pod'da dolu). Komut kaldırıldı; yerine (a) initContainer'ın kendi logu — ayrımı
> zaten o yapıyor — ve (b) uyumlu bir `kubectl run` tek-atışlığı, **iki yönde de
> ölçülmüş** (Service yokken `NXDOMAIN`, varken adres).
>
> **S2 — bu turun eklediği teşhis dalı CI logunda GÖRÜNMÜYORDU.** `kubectl logs
> job/tappa-migrate` init container'ı **asla** seçmez, ve yeni başarısızlık modunda
> `goose` konteyneri **hiç yaratılmamış** olur → `PodInitializing` BadRequest →
> satır sonundaki `|| true` onu yutar. Üç çağrı da konteyneri **adıyla** isteyen tek
> bir `migrate_logs()` yardımcısına çevrildi; *"Report what is running"* adımı da
> `-c tappa` **ve** `-c wait-for-postgres` okuyor.
>
> **S3 — sınır [GHCR çekme kimliği ömürlü]'nün durumu ölçülemiyordu, ve ürün ŞU AN
> o durumda.** `/healthz`+`/readyz` *"ayakta mıyım"* der, *"ayağa kalkabilir miyim"*
> demez. Ölçen komut runbook'a girdi (Secret'ın **değerine dokunmadan**, yalnız
> `managedFields[].time` ile pod `startTime`): canlı sonuç **`AT RISK`** — pod
> `2026-08-15T22:20:44Z`, Secret `2026-08-16T08:38:41Z`.
> ⚠️ **Ve bu kontrolün İLK yazımı yanlıştı ve tam ters cevabı verdi:** `[ "$a" \> "$b" ]`
> zsh'de `condition expected: >` ile düştü, else dalına düşüp `AT RISK` olan kümeye
> **`ok`** dedi. `sort` tabanlı, `sh` ve `bash` altında ayrı ayrı **ve ters kontrolüyle**
> doğrulanmış hâliyle değiştirildi. Aynı sınıf: *"yeşil raporlayan bozuk kapı"*.
>
> **S4** `verify-deployment.sh`'in *"bu noktada bir ağ bağlantısı YALNIZCA sunucu
> olabilir"* gerekçesi bu turda bayatladı (initContainer da 5432'ye TCP açıp startup
> paketinde `tappa_owner` taşıyor). **Bugün yanlış ateşlemiyor** ve sebebi zamanlama:
> initContainer ana konteyner doğmadan biter, kapı `rollout status` sonrası koşar,
> `pg_stat_activity` yalnız **canlı** backend listeler. Cümle daraltıldı ve neyin
> bozacağı yazıldı. **S5** migration arızasının bildirilme süresi ~37 sn'den ~390 sn'ye
> çıktı (3 × 120 + backoff); 600 sn deadline yetiyor ama pay ~565→~210 sn — sınır **[migration arızası artık ~10× geç bildiriliyor]**
> olarak sayıldı (o madde 5. turda bir numara kaydı — atıf **numarayla değil adla**
> verildiği sürece kaymanın önemi yok, ve numarayla verildiği için bir kez kaydı). **S6** sınır **[`postgres:17-alpine` hareketli etiket]** artık denetçinin doğruladığı ölçümü taşıyor (kubelet
> imaj GC'si **kullanımdaki** imajı geri alamaz).
>
> **Genel gözün 11 metin bulgusu da kapatıldı.** Öne çıkanlar: **G1** sınır **[`make audit` yerelde kırmızı]**
> *"CI'nin Go'su 1.26.5"* diyordu — `ci.yml:91` **`1.26.6`**, yani kapanmış bir açık
> açık sanılıyordu; madde düzeltildi (kırmızı olan yalnız geliştiricinin yerel Go'su).
> **G2** B1'in *"bu sınıf bir daha doğamaz"* iddiası yanlıştı: numaralı bir atıf
> (*"Sınır 12"*) hayatta kalmıştı, 13 satır altında doğru biçim kullanılırken —
> düzeltildi. **G3** sınır **[`postgres:17-alpine` hareketli etiket]**'in (c) gerekçesi yanlış soruyu cevaplıyordu (digest
> pinleme oran limiti için değil **içerik kimliği** için yapılır) **ve** (a)'nın
> *"iki yazım doğar"* kısmı yalnız benim değerlendirdiğim varyant için doğruydu:
> ölçüldü, `grep 'image:'` **üçünü de** `postgres:17-alpine`'da buluyor, yani üçünü
> birden pinlemek **tek yazım** bırakırdı. Taşıyıcı gerekçe (blast radius bir sağlık
> sondası) öne alındı, digest **açık sertleştirme** olarak duruyor. **G4** ayırt
> edici adresi basıyordu ama *"pod'un kendi adresiyle karşılaştır"* demiyordu (bayat
> EndpointSlice / CoreDNS 30 sn önbelleği bir DNS arızasını *"soket"* diye
> etiketletirdi) — her iki manifeste ve README'ye eklendi. **G5** *"Migration Job'ı ve
> sunucu bugün taşıyor"* **kümede yanlıştı** (canlı şablonlarda `initContainers` yok);
> cümle bir **ölçüm komutuyla** değiştirildi. **G7** dosyanın *"kümedeki tek iki
> NetworkPolicy"* cümlesi kendi kuralı listeye girince yanlışlandı (bugün **üç**).
> **G8** kartın *"toplam 31→30"* özeti ağaçtaki hiçbir değişikliğe karşılık gelmiyordu.
> **G6/G9** kozmetik ve *"tek bilinmeyen"* aşırı iddiası.
> ✅ Genel gözün **zamanlama saldırısı tutmadı** ve denetçi bunu kendisi söyledi:
> hata dalı 120 sn'de ateşliyor, T42'nin penceresi ~3 sn, o senaryoda dal hiç çalışmaz.
>
> **YENİDEN ÖLÇÜM.** Kum havuzu yeniden kuruldu ve yine silindi. Taze kurulum
> **üçüncü kez**: 17 → 26 → **33 sn, `RESTARTS=0`**. Her iki initContainer script'i
> `sh -n` temiz ve **hâlâ birebir aynı** (yalnız son kelime farklı). Her iki hata dalı
> düzenlemeden **sonra** yeniden koşuldu ve doğru dalı bastı.
>
> **🔴 [FAZ C · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED. AYNI KUSUR SINIFININ ÜÇÜNCÜ TEKRARI, VE BU KEZ
> O SINIFI KAPATMAK İÇİN YAZDIĞIM KONTROLÜN İÇİNDE.**
> Sıra şu: (1) kurtarma adımı yalnız **dolu** yolda test edildi, **boş** yol normal
> olandı → KRİTİK. (2) S3'ün v1'i at-risk kümede `ok` diyordu (`zsh` karşılaştırması).
> (3) S3'ün **v2'si dejenere girdide** `ok` diyor. **Üçünün şekli aynı: ölçülmeyen
> yol, gerçekte en olası olan yol.**
>
> **B1 — S3 v2, `secret/ghcr` HİÇ YOKKEN `ok` basıyordu.** Denetçi birebir koşturdu:
> `sec_t=""` → `newest`=`pod_t` → eşitlik testi false → `|| echo 'ok'`. Üç kabukta da
> aynı. **Ve bu, `AT RISK`'ten daha kötü bir durumu yeşil gösteriyordu:** Secret hiç
> yoksa pod yeniden çekemez, ezilmiş bir token bile yoktur. İki ek zayıflık da
> doğrulandı: **hassasiyet farkı cevabı ters çeviriyordu** (`…44Z` vs `…44.500Z` →
> `.` < `Z` → `ok`, yanlış) ve **`managedFields[-1:]` "en yeni" değil**, apiserver o
> diziyi önce Operation'a göre sıralıyor (bugün iki giriş de `Update` olduğu için
> doğru satırı okuyordu — *doğru çıkmak* ile *doğru olmak* aynı şey değil).
> **Çare README parçacığı değil, bir kapı:** `scripts/verify-deployment.sh
> pull-credential`, **tasarım gereği fail-closed** — okunamayan her girdi `UNKNOWN`
> (exit 2), ve `ok` yalnız iki damga da sabit biçime ayrıştırıldıktan sonra basılıyor.
> `managedFields`'ın **tümü** taranıp en yenisi seçiliyor; karşılaştırma
> `YYYYMMDDHHMMSS`'e indirgeniyor, yani hassasiyet farkı cevabı çeviremiyor.
> **On beş vaka gerçek script'e karşı koşuldu** (kubectl stub'ı ile): Secret yok ·
> `managedFields` yok · yalnız boş satır · pod yok · pod listesi boşluk · kubectl
> düşüyor · `startTime` boş · **her iki taraf da okunamıyor** → **sekizi de exit 2**,
> hiçbiri `ok` değil; Secret yeni/eski/eşit · karışık hassasiyet **iki yönde** ·
> `managedFields` ters sırada · iki pod'dan biri riskli → yedisi de doğru.
> Canlı: **`AT RISK`, exit 1**. Sayılmış kalıntı: **aynı saniye** içindeki fark `ok`
> sayılıyor (k8s damgaları zaten saniye hassasiyetinde, ölçüldü).
>
> **B2 — canlı bir deploy kapısı, sorgusu patladığında YEŞİL geçiyordu.**
> `psql`, `-v ON_ERROR_STOP=1` olmadan stdin'den okurken SQL hatasında **exit 0**
> veriyor. Gerçek PostgreSQL **17.10**'a karşı ölçüldü — bozuk sorgu: bayrak yokken
> **exit 0**, bayrakla **exit 3**; sağlıklı sorgu ikisinde de **exit 0**. Yani
> `pg_stat_activity`'nin şeması ya da izni değişseydi kapı **hiçbir şey
> karşılaştırmadan** *"her ağ bağlantısı tappa_app"* diyecekti. Bayrak eklendi **ve
> başarısızlık açıkça yakalanıyor** (`set -e`'ye bırakılmadı): uçtan uca ölçüldü —
> sağlıklı şema → all-clear + exit 0 · sorguyu kasten bozunca → *"NOTHING was
> checked. This is not a pass."* + **exit 2**, ve all-clear satırı **basılmıyor**.
> Bu, dosyanın **kendi başlığının** tarif ettiği kusurun üçüncü örneğiydi.
>
> **D1** kurtarma adımının `kubectl` düşünce deploy'u durdurması **doğru varsayılan**
> ama sayılmamıştı → sınır **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]**. **D2** kartın *"14408 / 20930 bayt"* ölçümü
> **yeniden üretilemiyor** (bugün 14818 / 21340; dosyalar tur boyunca büyüdü) — bayat
> mutlak sayı kaldırıldı, taşıyıcı ölçüt bırakıldı. **D3** *"`tappa.everva.com.tr`
> bugün zaten Cloudflare üzerinden cevap veriyor"* artık **yanlış**: `dig` →
> `144.76.158.60`, kapı → **exit 0 "not proxied"**; joker **hâlâ** proxy'li, yani
> uyarının özü ayakta, düzeltilen yalnız bu host hakkındaki şimdiki zaman.
> **D4** Cloudflare kapısının başarı özeti `^(HTTP|…):` arıyordu ama durum satırı
> `HTTP/2 200` — iki nokta yok, yani **hiçbir zaman basılmıyordu**; `^HTTP/` oldu ve
> canlıda artık görünüyor. **D5** `dnscheck` için temizleme komutu eklendi (`--rm`
> yalnız kubectl bağlıyken siler, komut ise Ctrl-C ihtimali yüksek bir olayda
> koşulur). **D6** README tablosu elimdeki **üç** koşunun **ikisini** gösteriyordu;
> üçü de yazıldı.
>
> **🔴 [FAZ C · 2026-08-16] 6. TUR — YENİ ÜÇÜNCÜ GÖZ RED. AYNI SINIFIN DÖRDÜNCÜ ÖRNEĞİ, AYNI KONTROLÜN
> ÜÇÜNCÜ SÜRÜMÜNDE — VE KAPATMAYI BIRAKIP SAYMAYA GEÇTİM.**
>
> **BULGU.** `check_pull_credential`'ın Secret tarafı, ayrıştıramadığı bir
> `managedFields` girdisini **sessizce atlıyordu** (`continue`), oysa pod tarafı aynı
> girdi sınıfında `return 2` veriyordu. En yeni girdi okunamazsa **eski bir damga
> kazanıyor** ve at-risk küme `ok` çıkıyordu. Yeniden ürettim:
> `SEC_OUT=$'2026-08-14T00:00:00Z\\n\\n'` → **`ok … EXIT=0`**. İkinci kanal: `_norm`
> ilk 14 rakama kestiği için **saat dilimi ofsetini UTC sanıyordu** — pod
> `2026-08-16T09:00:00+02:00` (= 07:00Z) vs secret `08:38:41Z` → gerçek cevap
> **AT RISK**, script **`ok`**. 🔴 **Ve bunların ikisi de, bir önceki turda kendi
> yazdığım *"fail-closed by construction — okuyamadığı HER girdi için UNKNOWN"*
> cümlesini, o cümlenin KIRK SATIR AŞAĞISINDA çürütüyordu.**
>
> **KARAR: 3. YOL — HÜKÜM KALDIRILDI, KANIT KALDI.** Gerekçe brief'in ikinci durma
> kuralı: *"Bir noktadan sonra her koruma bir sonraki turda yeniliyorsa: kapatmayı
> bırak, dürüstçe LİMİT olarak yaz. Sayılmış bir açık, kapatıldığı İDDİA EDİLEN bir
> açıktan güvenlidir."* Aynı korumanın **üç kez** yenilenmesi o noktanın ötesidir.
> Elenen iki yol ve nedenleri: **(1) yapısal kapatma** — bunu bir önceki turda
> **yazdım** ve çürüdü; aynı iddiayı dördüncü kez üretmek, kanıtı değil özgüveni
> tekrarlamak olurdu. **(2) daraltma** — iddiayı gerçeğe indirmek doğru olurdu ama
> tehlikeli olan şey damgalar değil **`ok` kelimesi**: yanlış bir hüküm operatörü
> *aramayı bırakmaya* ikna eder, ve daraltılmış bir hüküm de hâlâ bir hükümdür.
> **Seçilen (3):** komut iki damga kümesini tek biçimde basıyor, kuralı yazıyor,
> karşılaştırmayı **operatöre bırakıyor**. Çıkış kodu yalnız *okuyabildim mi* der
> (`0` = kanıt basıldı, **güvendesin demek değil**; `2` = okunamadı, **hiç kanıt
> basılmadı**). Komut zaten `deploy.yml`'e bağlı değil (19 adımın hiçbiri çağırmıyor),
> yani kaybedilen bir kapı yok.
>
> **VE BU SÜRÜMÜN DOĞRULUK İDDİASI TARTIŞMAYLA DEĞİL SAYIMLA VERİLİYOR** — önceki üç
> sürümün her biri iddiasını akıl yürütmeyle kurmuştu ve her biri kaybetti:
> fonksiyonda `ok`/`AT RISK`/`safe` geçişi **0** · dokuz `return`'ün **dokuzu da**
> `return 2` · iki damga arasında karşılaştırma **yok** (tek `-gt`, `managedFields`
> **girdi sayısı** kontrolü). Bunlar `grep -c` ile doğrulanabilir, savunulması
> gereken cümleler değil.
> **On altı vaka gerçek script'e karşı koşuldu:** v3'ü düşüren iki kanal · Secret yok ·
> `managedFields` boş · girdi/satır sayısı uyuşmuyor · pod yok · yalnız boşluk ·
> `kubectl` düşüyor · zaman aşımı `rc=124` · `startTime` boş · pod adı boş · iki
> haneli yıl · `Z` taşımayan damga · üç pod'un ortası okunamaz → **on dördü de
> exit 2 ve hiçbiri kanıt basmadı**; iki okunabilir girdi kanıt bastı. Ayrıştırma
> artık normalleştirme değil **kabul/ret**: `Z` ile bitmeyen hiçbir damga geçmiyor,
> yani ofset kanalı bir daha *sessizce yanlış* olamaz — reddediliyor.
>
> **U1** README *"İki ayrı taze koşu"* diyordu, tablo **üç** sütun taşıyordu (5. turun
> D6'sı sütunu ekleyip cümleyi bırakmıştı) — 4. turun U5'inin **aynı bölümde** yeniden
> doğuşu; düzeltildi. **U2** kartın kendi içinde numaralı bir sınır atıfı kalmıştı
> (*"sınır 15"*, bugün 16) ve dördü daha bulundu; hepsi **anahtar adla** verildi.
> 🔴 **Ve *"yani bu sınıf bir daha doğamaz"* cümlesi geri çekildi** — iki kez
> yanlışlandı (4. turda G2, 6. turda U2), yani bir ölçüm değil bir tahmindi. Yerine
> dar ve doğru olan yazıldı: adla verilen atıflar numara kaymasından etkilenmez.
> Mekanik kontrol eklendi: dokuz anahtarın tamamı var olan maddeye düşüyor, **sıfır
> sarkan atıf** — bu tarama iki gerçek kusur buldu (`deploy.yml`'de satır sarması
> yüzünden **ikiye bölünmüş** bir anahtar, ve script'te **harf çevrilmiş** bir
> anahtar), ikisi de düzeltildi. **U3** `all` modu `pull-credential` koşmuyordu ve
> hata mesajı onu listelemiyordu: mesaj düzeltildi, `all`'a **eklenmedi** — `all`
> kapıları koşar, bu bir kapı değil, ve *"hepsi koştu"* anlamına gelen bir ada
> vermediği güvenceyi vaat ettirmek bu kartın imza kusurudur.
>
> **🔴 [FAZ C · 2026-08-16] 7. TUR — SINIFIN BEŞİNCİ ÖRNEĞİ: HÜKÜM KODDAN KALKTI,
> RUNBOOK'TA YAŞIYORDU.** Denetçi kod tarafını bağımsız olarak yeniden ölçüp
> kapandığını doğruladı (kendi `grep`'i, kendi fonksiyon sınırı, kendi 10 vakası:
> 8'i exit 2 ve hiç kanıt basmadan). **Tek bloklayan metindi ve ağırdı.**
>
> `README`'nin sınır **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** maddesi
> operatöre *"`pull-credential` koş; **`AT RISK` diyorsa** çare yeni bir deploy"*
> diyordu. O çıktı bu turda **kaldırıldı** — ve **aynı dosya** kırk satır yukarıda
> *"`ok`/`AT RISK`/`safe` geçişi: 0"* diye kendi ölçümünü yazıyordu. Yani README
> kendi içinde çelişiyordu, ve **bekleme kelimesi hiç gelmeyeceği için** operatör
> *"demek ki sorun yok"* deyip kümeyi at-risk hâlde bırakabilirdi — üstelik sınır 
> **[kurtarma adımı …]**'nın tarif ettiği *"yeni token, eski iş yükü"* anında, yani
> komutun **en çok işe yaradığı** anda.
>
> 🔴 **VE ÖNEMLİ OLAN ŞU: bu turda yazdığım mekanik tarama bunu YAKALAYAMAZDI.**
> Tarayıcı *köşeli parantezli anahtar atıflarını* sayıyordu, **hüküm sözcüklerini**
> değil — yani tarayıcıyı **bir önceki kusurun şekline** göre yazmıştım, o turun
> **ürettiği** şekle göre değil. Sınıfın beş tur boyunca hayatta kalma mekanizması
> tam olarak bu. Doğru tarama (`grep -rn 'AT RISK\|AT-RISK'`, operatörün okuduğu
> **her** dosyada) koşuldu: `README:298` ölçümün kendisi ✔ · `verify-deployment.sh`
> ve kartta tarih anlatımı ✔ · `deploy.yml` ve manifestlerde **0** ✔ · tek talimat
> `README:639`'daydı ve **kaldırıldı**. Sayım da README'ye taşındı — sayılan bir
> açığın güvenli olması, sayımın **operatörün okuduğu yerde** durmasına bağlı.
>
> **C1 — `all` modu ilk kapı düşünce ikinciyi hiç koşmuyordu.** `set -e` altında
> `check_cloudflare …; check_db_role …`. Ölçüldü: proxy'li host sahtelenince
> `db-role` **hiç çalışmadı**. Fail-open değil (koşu kırmızı) ama *"hepsi"* adı
> sağlamadığı bir bütünlük vaat ediyordu. İkisi de koşuyor, sonuçlar toplanıyor,
> özet satırı basılıyor; çıkış kodu **ihlal (1) > okunamadı (2) > temiz (0)**.
> Dördü de ölçüldü: `VIOLATION+pass→1` · `pass+pass→0` · `pass+UNCHECKED→2` ·
> `VIOLATION+UNCHECKED→1`. ⚠️ İlk yazımım bozuktu — `"$( )"` içine gömülü `case`
> çevreleyen `case`'i bozdu ve özet satırı **kendi kaynak metnini** bastı; ölçüm
> yakaladı, adlandırılmış bir yardımcıya çevrildi.
> **C5** `db-role`'ün özet sorgusu düşerse mesajsız `exit 3` ile ölüyordu (fail-closed
> ama açıklamasız); artık *"the offender query passed but the summary query did not
> run, so this result is NOT trustworthy"* + **exit 2**. İki yol da ölçüldü.
> **C2** *"kartın içindeki numaralı atıfların hepsi çevrildi"* iddiam eksikti — iki
> tane daha vardı (`:509`, `:569`), ikisi de çevrildi; kalan dört geçiş **geri çekme
> metninin kendisi**. **C3** emsal 1 sn kayıktı: canlı Job'dan okundu
> (`startTime 08:38:54Z` → `Failed 08:39:32Z`) = **38 sn**, 37 değil.
> **C4** defterde **iki blok aynı numarayı** taşıyordu (*"4. TUR"*) ve 3/4 sırasız
> görünüyordu; her başlık artık **hangi yarıya ait olduğunu ve tarihini** taşıyor
> (`[PAKETLEME yarısı · 2026-08-15]` / `[FAZ C · 2026-08-16]`), ve sıranın neden
> böyle olduğu yazıldı — bu defteri sıfır hafızalı bir ajan okuyacak.
>
> **✅ [FAZ C · 2026-08-16] 8. TUR — `tappa-security-auditor` ONAY.** (2. turun
> kritiğini bulan mercek.) Üç bulgu, üçü de tek satırlık, üçü de kapatıldı.
>
> **§4.7 (ORTA) — yakalanan stderr basılıyordu.** `kubectl`'in jsonpath yazıcısı bir
> **şablon çalıştırma hatasında** şablonu değil **nesnenin tamamını** stderr'e döküyor.
> Mekanizma zararsız bir ConfigMap üzerinde ölçüldü (Secret'a **dokunulmadan**):
> `-o jsonpath='{.data[0]}'` → *"object given to jsonpath engine was: …
> "ca.crt":"-----BEGIN CERT…"*. `secret/ghcr` için o içerik `.dockerconfigjson`, yani
> **canlı çekme token'ı**, ve basıldığı an `deploy.yml`'in kendi koyduğu *"Report the
> SHAPE and never the value"* kuralı delinirdi. **Ulaşılabilirlik kanıtlanamadı**
> (gerçek bir Secret'ın `managedFields`'ı daima dizidir, yani dökümü tetikleyen tip
> uyuşmazlığı bariz biçimde erişilebilir değil) — ama `README`'nin kalıcı çaresi
> *"uzun ömürlü `read:packages` PAT"*, ve **o gün geldiğinde bu kanal iptal edilmiş
> bir iş token'ı değil YAŞAYAN bir kimlik taşır**. Üç `kubectl` çağrısının **üçü de**
> (denetçi ikisini işaretlemişti; üçüncüsü pod listesi, aynı kalıp, aynı muamele —
> dosyada tek kural olsun diye) `2>/dev/null` + sabit metne çevrildi. Ölçüldü, sahte
> bir token döken stub ile: **eski yazım 1 kez sızdırıyor, yeni yazım 0**; canlı yol
> hâlâ çalışıyor ve çıktısında `dockerconfigjson` **0**. Taranan dördüncü yakalama
> (`curl`) **değiştirilmedi ve gerekçesi yazıldı**: farklı araç, nesne dökümü değil
> mesaj basıyor, ve bu turun eklediği bir satır değil.
>
> **§4.6 (DÜŞÜK) — `delete pod` bayrağı.** `get` ile `delete` arasında pod kaybolursa
> NotFound → exit 1 → `set -e` → adım kırmızı; ve adım tam olarak 2. turun kritiğinin
> durduğu yerde, `secret/ghcr` ezildikten **sonra**, hiçbir `apply`'dan **önce**. Yani
> deploy **iyi huylu** bir sebeple kümeyi *"yeni token, eski iş yükü"* durumunda
> bırakıyordu. Ölçüldü: bayraksız `Error from server (NotFound)` **exit 1**,
> `--ignore-not-found` ile **exit 0**. Aynı dosya bu sınıfı 68 satır aşağıda zaten
> tanıyordu. `README` sınırı da güncellendi: **`delete` yarısı kapatıldı**, `get`
> yarısı sayılmış kalmaya devam ediyor (orada kırmızı olmak doğru).
>
> **§4.5 (DÜŞÜK) — uygulama pod'u artık `tappa_owner` adını ağa yazmıyor.** Kriter
> ihlal edilmiyordu (kimlik doğrulaması yok, DSN yok, parola yok) ama iki bedeli
> vardı ve ikincisi ağırdı: `verify-deployment.sh`'in `db-role` kapısı
> `usename='tappa_owner' AND client_addr NOT NULL` üzerine kurulu ve mesajı *"tenant
> isolation is void"* diyor — yani **ürünün kendi sağlık sondası**, §4.5'i bekleyen
> tek kapıya bir yanlış alarm kanalı açıyordu. Rol adı yalnız bir dize olduğu için
> `-U tappa_app` bedava aynı işi görüyor. **Ölçüldü** (docker, `tappa_app` rolü o
> veritabanında **hiç yokken**): `-U tappa_app` → **`accepting connections`, exit 0** ·
> `-U tappa_owner` → aynı · **`-U` hiç yokken, uid 65532** → `no attempt`, **exit 3**
> (yani bayrağın kendisi hâlâ zorunlu) · ve hiçbir sonda arkasında **backend
> bırakmıyor**. `30-migrate-job.yaml` **`tappa_owner` ile kaldı** — o pod gerçekten o
> rol. İki script bunun dışında hâlâ birebir aynı.
>
> **VE DENETÇİ KARTIN AÇIK BİR BULGUSUNU DARALTTI — buraya yazılıyor.** Kartın
> *"`redline-check.sh`'in `SRC` listesi `deploy/`'u görmüyor"* maddesi bu turda
> **yanlış yeri** işaret ediyor: bu turun tek gerçek §4.7 kanalı **zaten taranan**
> bir yolda (`scripts/`) duruyordu ve tarayıcı **yine de göremezdi**, çünkü R7 deseni
> Go'ya özgü (`(slog|log|fmt)\\.[A-Za-z]+\\(`) ve bir kabuğun
> `echo "$captured_stderr"` şeklini **hiç tanımıyor**. **Eksik olan dizin değil,
> desen şekli** — **ve bu cümle YALNIZ R7 İÇİN geçerlidir, kapsamı burada yazılıyor.**
> Ölçüldü: `deploy/`'a R7 deseninin eşleşmesi **0**, yani bu turun kanalı `SRC`
> genişletilse de görünmezdi. **Ama `SRC`'ye `deploy/` eklemek "hiçbir şey bulmaz"
> DEĞİLDİR:** R5'in `DATABASE_MIGRATE_URL` deseni `deploy/` + `Dockerfile`'da **11**,
> N1'in Node deseni **1** eşleşme veriyor — yani bu kartın **700 satır yukarıdaki**
> *"naif bir genişletme 12 yanlış pozitif üretiyor"* ölçümünün **aynısı**, ve o
> ölçüm hâlâ geçerli. İki cümle çelişmesin diye: R7 için genişletme yetmez (desen
> Go'ya özgü ve bir kabuğun `echo "$captured_stderr"` şeklini tanımıyor); R5/N1 için
> genişletme **erken**, çünkü önce o 12 yanlış pozitifin ne anlama geldiğine karar
> verilmeli. Orkestratöre devredilen madde bu iki cümleyle birlikte okunmalı.
>
> **🔴 8. TURUN İKİ KENDİ HATASI — DEFTERE, SOHBETE DEĞİL.** Kartın kendi emsali
> (*"SÜREÇ İHLALİ, KÖK NEDENİYLE"* ve 4. turun `B4`'ü) bunları kaydediyor; bu tur da
> kaydetmeli, yoksa defter yalnız başarıyı taşır.
> **(a) SALT-OKUMA SINIRI AŞILDI.** `--ignore-not-found`'un davranışını ölçmek için
> `ns/tappa`'ya **iki kez `kubectl delete pod`** koşturdum. Ad kasten var olmayan bir
> addı (`definitely-not-a-real-pod-9x7q`), yani **hiçbir şey mutasyona uğramadı** —
> orkestratör kümeyi doğruladı (5 pod, 1 netpol, canlı 200/200, zarar yok) — ama
> `delete` fiili o namespace için **yasaktı** ve ölçüm bir kum havuzunda yapılmalıydı.
> Kural, sonucun iyi çıkmasıyla değil, fiilin kendisiyle ilgilidir.
> **(b) BİR DOĞRULAMA GEÇERSİZDİ VE ÖNCE FARK EDİLMEDİ.** İlk `-U tappa_app` ölçümümde
> konteyner IP'sini `{{.NetworkSettings.IPAddress}}` ile aradım, boş döndü, ve **iki
> sonda da boş bir host'a** gitti: ikisi de `no response`/exit 2 verdi, yani *"her iki
> rol de çalışmıyor"* gibi görünen simetrik ve **anlamsız** bir sonuç. Simetri beni
> uyardı; olmasaydı bu, ölçmediğim bir şeyi ölçtüm sanacağım bir sonuçtu. Ayrı bir
> docker ağıyla yeniden koşuldu. **Ders 4. turun B4'ünün aynısı: bir ölçümün ÇIKTI
> ÜRETMESİ, doğru şeyi ölçtüğü anlamına gelmez.**
>
> **✅ [FAZ C · 2026-08-16] 9. TUR — SON. Üçüncü göz ONAY, güvenlik denetçisi ONAY,
> birinci durma kuralı karşılandı** (art arda iki tur davranış kusuru yok; denetçi
> §4.7 sınıfının **altıncı örneğini kodda aradı ve bulamadı**, ve en olası adayı —
> `deploy.yml`'in `secret/ghcr` üzerindeki yönlendirilmemiş `go-template` çağrıları —
> **ölçümle eledi**: go-template yazıcısı nesne dökmüyor). Altı metin/kabuk maddesi:
> **B1** bu turda eklediğim teşhis komutu **yazıldığı gibi çalışmıyordu** —
> `get svc tappa-postgres endpointslice` her iki adı da `svc` kaynağında arıyor →
> `services "endpointslice" not found`, **exit 1**, yani bölüm 5'in *"EndpointSlice
> boş"* teşhisi hiç yapılamıyordu **ve hata aranan arızanın kendisi gibi
> okunabiliyordu** — üstelik bölümün başında *"her satır fiilen ölçüldü"* yazıyor.
> İki ayrı çağrıya bölündü (ölçüldü: ikisi de exit 0).
> **B2** `all` modu **ölçemediği kapıyı `pass` diye etiketliyordu**: ad çözülmediğinde
> cloudflare *"Nothing about Cloudflare could be checked"* deyip 0 dönüyor, `all` de
> `cloudflare=pass`, **exit 0** basıyordu — kendi yorumu *"2 if any gate could not
> CHECK"* derken. Aynanın öteki yüzü: curl zaman aşımı (rc=28) → `VIOLATION`, oysa o
> da ölçememe. Artık kelime **`CF_OUTCOME`**'dan geliyor, çıkış kodundan değil; altı
> kombinasyon ölçüldü (`pass/pass→0` · `VIOLATION/pass→1` · `UNCHECKED/pass→2` ·
> `timeout→UNCHECKED/2` · `pass/UNCHECKED→2` · `UNCHECKED/UNCHECKED→2`) ve
> **`cloudflare` modunun kendi sözleşmesi değişmedi** (0/1/0/1 — `deploy.yml` ona
> bağlı, özellikle *"ad çözülmüyor → 0"* ilk deploy için).
> **U3** `-c wait-for-postgres` komutları bugünkü kümede `container … is not valid
> for pod … out of: goose` veriyor (FAZ C henüz inmedi); uyarı 50 satır ötedeydi ve
> bölüm 4'te hiç yoktu — **her iki komut bloğunun yanına** taşındı.
> **U4** `README:95`'in doğrulama komutu sağlıklı durumda **sıfır çıktı + exit 1**
> veriyordu (origin `server` başlığı göndermiyor, durum satırında `HTTP` sonrası iki
> nokta yok) — `^HTTP/` biçimine hizalandı, ölçüldü: `HTTP/2 200`, exit 0.
> **U1** kart kendi ölçümüyle çelişiyordu ve **cümle R7'ye daraltıldı** (yukarıda).
> **U2** bu iki kendi hatam yukarıya yazıldı.
> ⚠️ **Denetçinin doğrulayamadığı tek şey kayda geçsin:** `--ignore-not-found`'un
> bayrak semantiği salt-okuma sınırı yüzünden **denetçi tarafından koşturulmadı**;
> akış stub matrisiyle doğrulandı, bayrağın kendisi yalnız benim ölçümümle.
>
> **KAPSAM DIŞI BIRAKILAN, BİLİNÇLİ:** kartın *"Yedek: otomatik, geri yükleme
> denenmiş"* ve *"KEK dönme aracı"* kriterleri **hâlâ açık** (FAZ D); `README`'nin
> sınır listesinde **[yedek YOK]** ve *"KEK döndürme aracı YOK"* olarak duruyorlar
> ve **kaldırılmadılar**.
>
> **KUM HAVUZU SAPMALARI (dördü de yazılı):** namespace `tappa-verify` · PVC
> 20Gi→2Gi (node diski; ölçülen şey ağ ve sıra, depolama değil) · atılabilir sırlar
> (`openssl rand`, değerler hiç basılmadı) · 🔴 **iki ürün imajı `postgres:17-alpine`
> stand-in'leriyle değiştirildi**, çünkü yukarıdaki KEP-2535 ölçümü gereği kum havuzu
> pod'ları düğümdeki private imajları **kullanamıyor** ve `read:packages` yetkili bir
> token yok (yerel `gh` token'ının kapsamları: `gist, read:org, repo, workflow`;
> GHCR manifest isteği → **401**). Stand-in'ler test edilen özelliği koruyor
> (açılışta hemen DB'ye bağlan, başarısızsa çık) ama **goose ikilisinin kendisini
> kanıtlamıyor** — o zaten 2026-08-15'te kural silindiğinde 20 migration'ı geçmişti.
> `40-ingress.yaml` kum havuzunda **uygulanmadı** (canlı host ile çakışırdı).

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
