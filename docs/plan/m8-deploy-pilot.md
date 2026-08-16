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
>
> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ D — "deploy YOLU doğru olsun"): T44 KAPANDI,
> T43 KÖKÜNDEN KALDIRILDI, VE BU BLOK YUKARIDAKİ ÜÇ CÜMLEYİ ADIYLA İPTAL EDİYOR.**
> Değişen dosyalar: **yeni** `deploy/k8s/01-rbac.yaml` · `.github/workflows/deploy.yml` ·
> `deploy/k8s/{20-app,30-migrate-job}.yaml` · `deploy/README.md` ·
> `scripts/verify-deployment.sh` · `Dockerfile` (yalnız başlık yorumu). **Go kodu
> değişmedi** (`git status --short` → 0 adet `.go`), `ci.yml` **değişmedi**. Canlı ürün
> üç ayrı anda ölçüldü: `/healthz` **200**, `/readyz` **200**. Küme mutasyonu yalnız
> kum havuzu `tappa-verify`'de yapıldı ve namespace iş sonunda **silindi** (artık
> kalıntı: `ns` → NotFound, `clusterrole|clusterrolebinding` eşleşmesi **0**);
> `ns/tappa`'ya hiçbir yazma yapılmadı.
>
> **İŞ 1 — T44: RBAC ARTIK BİR DOSYA, VE ÖLÇEREK DARALTILDI.**
> Elle yaratılmış (yalnız kümede yaşayan) beş nesne repoya alındı. ⚠️ **Orkestratörün
> ölçümü eksikti ve düzeltiliyor: üç değil BEŞ nesne var** — brief `Role`, `RoleBinding`
> ve `ServiceAccount`'u sayıyordu; kümede ayrıca `ClusterRole/tappa-namespace-only` ve
> onun `ClusterRoleBinding`'i de vardı (`namespaces`, `resourceNames: [tappa]`,
> `get,patch,update`). Beşi de `01-rbac.yaml`'a girdi.
>
> **Daraltma, satır satır ve ölçümle.** Yöntem: kum havuzunda **yalnız önerilen
> kurallara** bağlı bir ServiceAccount yaratıldı ve `deploy.yml`'in küme çağrıları
> o kimlikle (`--as=...`) baştan sona koşuldu.
> ⚠️ **Burada bir tur boyunca *"deploy.yml'in on beş küme çağrısı"* yazdı ve bu iki
> şeyi karıştırıyordu.** **15**, benim prova betiğimde numaralandırdığım **adım
> sayısıydı** (`01`…`15`), `deploy.yml`'in çağrı sayısı değil. Doğru sayı, yöntemiyle
> birlikte: her `run:` gövdesi ayrıştırılıp yorumlar atılarak, **API sunucusuna giden**
> `kubectl` çağrıları sayıldı — `kubectl version --client`, `kubectl config
> current-context` ve `--dry-run=client` ile yerelde render edilenler **hariç**;
> `verify-deployment.sh db-role`'un `exec`'i **dahil**. Sonuç **19**: adım 14→2 ·
> 15→5 · 16→5 · 17→3 · 18→1 · 19→3. (Bağımsız denetçi de aynı 19'u saydı.) Prova
> betiğinin 15 adımı bu 19 çağrının **hepsinin şeklini** kapsıyordu — bir `for`
> döngüsündeki iki `logs` çağrısı ve iki `get job` yoklaması provada birer kez
> koşuldu — ama *"on beş çağrı"* demek yanlıştı. Kaldırılanlar ve *neden gerekmediği*:
>
> | Çıkarılan | Neden — hangi adım istemiyor |
> |---|---|
> | `secrets` create/update/patch/delete/**list**/watch | Secret yazan adım (**"Write the GHCR pull credential"**) kalktı; geriye tek varlık kontrolü kaldı → `get` + `resourceNames: [tappa-secrets]`. Ölçüm: `get tappa-secrets` → NotFound (yetkili) · `get some-other-secret` → **Forbidden** · `get secrets` (list) → **Forbidden** |
> | `pods` create/update/patch/watch/**get** | Hiçbir adım Pod yaratmıyor (Pod'ları denetleyiciler yaratır, kendi kimlikleriyle). `get` de gereksiz çıktı: `logs job/…`, `exec statefulset/…` ve `get pods` **üçü de LIST üzerinden** çözüyor — yalnız `list` bırakılıp üçü de koşturuldu, üçü de exit 0 |
> | `pods` **delete** | Onu kullanan adım (takılmış-pod kurtarma) kalktı. `delete job --wait=true` yine de çalışıyor: Job'ın pod'larını **çöp toplayıcı** siliyor (ölçüldü, kimlikte pod-delete yokken exit 0) |
> | `persistentvolumeclaims` (yedi fiil) | PVC `volumeClaimTemplates`'ten doğar, statefulset denetleyicisi yaratır. Kum havuzunun StatefulSet'i volumeClaimTemplate taşıyordu ve **hiç PVC yetkisi olmadan** uygulandı |
> | `serviceaccounts` (yedi fiil) | `deploy.yml` hiçbir ServiceAccount'a dokunmuyor |
> | `events` (yedi fiil) | Hiçbir adım event okumuyor; `kubectl describe` dosyada **geçmiyor** |
> | `replicasets` (yedi fiil) | `rollout history`/`undo` içindir, ikisi de iş akışında yok. `rollout status deployment` ve `logs deployment/…` **replicaset yetkisi olmadan** koştu |
> | `deployments/status`, `statefulsets/status` | Status alt kaynağını denetleyiciler yazar; `rollout status` `.status`'ü **ana kaynaktan** okur |
> | `external-secrets.io/externalsecrets` | `deploy/examples/externalsecret.example.yaml` **örnektir**; `deploy.yml` yalnız `deploy/k8s/` altındaki adlı dosyaları uyguluyor ve hiçbiri ExternalSecret değil |
> | `jobs` **patch** | Migration adımı Job'ı **önce siliyor** (`--wait=true`), yani apply daima `create`. Kaldırmanın bedeli ölçüldü ve **sonucu değiştirmiyor**: Job silinemeden kalmışsa `patch`'siz *"cannot patch resource jobs"* (exit 1), `patch`'liyse API'nin *"spec.template … is immutable"*'ı (exit 1) |
> | `namespaces` **create** | Namespace operatörün (RBAC onun içine giriyor). `deploy.yml` 00-namespace.yaml'ı **var olan** nesneye yamalıyor |
>
> **Bırakılan her yetkinin sahibi bir adım var** ve dosyada her kuralın üstünde adıyla
> yazılı. İkisi kolay kaçırılıyor, o yüzden burada da:
> **(1) `apps` üzerinde `watch` ZORUNLU ve yokluğu FAIL-OPEN.** Üç ölçüm, sağlıklı ve
> bitmiş bir rollout üstünde: `get,create,patch` → `rollout status` **exit 1, "timed out
> waiting for the condition"** (sağlıklı deployment'ta!) · `get,list,create,patch` →
> **exit 0 ama** stderr'de `Failed to watch … is forbidden` — yani **hiçbir şey
> izlemeden** ilk LIST ile "başarılı" dedi · `get,list,watch,create,patch` → exit 0,
> gerçek. Ortadaki satır tehlikeli olan.
> **(2) `namespaces` üzerinde `patch` ZORUNLU, ve saf test bunu GİZLİYOR.** Değişmemiş
> bir `00-namespace.yaml`'ı yalnız `get` ile apply etmek **"unchanged", exit 0** verir
> (kubectl boş yama hesaplayıp hiç göndermez). Gerçekten değişen bir etiketle: `get`
> tek başına **exit 1**, `get,patch` → **"configured", exit 0**.
>
> **`pods/exec` KARARI — ölçümle verildi, ve verilen karar (i)'dir.**
> Kapı doğduğu günden beri **hiç koşmamıştı**: `pods/exec` ayrı bir alt kaynak ve elle
> yazılmış rolde yoktu; önceki her deploy migration'da düştüğü için 19. adıma hiç
> ulaşılmamıştı. **Elenen (ii) — kapıyı exec'siz yeniden yazmak:** hiçbir yetki
> kaldırmıyor (kimlik zaten `jobs: create` taşıyor, migration'ın kendisi budur) ve
> bedeli somut: iş akışının içinde yazılmış yeni bir pod spec'i, veritabanı parolasının
> **CI'ın yazdığı bir manifestten** geçmesi (bugün olmayan **yeni** bir §4.7 kanalı —
> exec biçimi `PGPASSWORD`'ü Postgres konteynerinin **kendi ortamından** okuyor ve ona
> hiç dokunmuyor), bir bekleme döngüsü, log okuma ve temizlik: **beş RED'i kabuk
> dalları yüzünden almış** bir dosyada ~40 satır yeni kabuk. **Seçilen (i)** ama
> `resourceNames: ["tappa-postgres-0"]` ile: exec **tek pod adına** kilitli. Ölçüldü:
> `exec statefulset/tappa-postgres` → **exit 0**; `exec deployment/tappa` →
> **Forbidden** — yani `TAPPA_TAG_KEK`'i ortamında taşıyan uygulama pod'una kabuk
> **yok**. Set ölçeklenirse yeni ordinal reddedilir ve kapı **fail-closed** olur.
>
> 🔴 **MARJİNAL-YETKİ ARGÜMANI HİSSİYAT DEĞİL, UÇTAN UCA ÖLÇÜLDÜ — ve sonucu bir
> SINIR olarak yazıldı.** Kum havuzunda, **yalnız** daraltılmış kurallara bağlı
> kimlikle: `secretKeyRef` ile bir Secret'a bakan bir `Job` yaratıldı ve o Secret'ın
> değeri `pods/log` üzerinden **okundu** — üstelik aynı kimlik o Secret'ı
> **listeleyemiyor**. Yani bir Job'ın pod şablonu kendi namespace'indeki her Secret'a
> referans verebiliyor (bu referansın RBAC'i yok, kubelet çözüyor) ve deploy,
> başarısız migration'ı raporlamak için o logu okumak **zorunda**. **Sonuç:
> `secrets: get` (tek isim) ve `pods/exec: create` (tek pod) yeni bir erişim
> AÇMIYOR;** daraltmanın kazandırdığı, yıkıcı yarının kalkmasıdır — kimlik artık
> `tappa-secrets`'ı yaratamaz, ezemez, silemez, yani **yeniden üretilemeyen** tek
> değeri yok edemez. `deploy/README.md` sınır
> **[deploy kimliği namespace'in sırlarını okuyabilir]**.
>
> 🔴 **VE BU TURUN KENDİ ÖLÇÜM TUZAĞI, DEFTERE:** `kubectl auth can-i` **alt
> kaynakları görmüyor** ve "yes" diyor. `can-i create pods/exec` → **yes** ·
> `pods/portforward` → **yes** · **`pods/nonsense` → yes** (kontrol: öyle bir alt
> kaynak yok). Doğrusu `can-i create pods --subresource=exec` → **no**, kontrolü
> `--subresource=nonsense` → **no**. İlk ölçümüm eğik çizgili biçimdeydi ve *"yetki
> zaten var"* diyordu — kontrol sorusu olmasa **yanlış bir sonuca** varacaktım. Bu
> yüzden `deploy.yml`'e `auth can-i` ön kontrolü **eklenmedi** (README sınır
> **[`kubectl auth can-i` alt kaynakları GÖRMÜYOR]**); yokluğun teşhisi bunun yerine
> ilk küme çağrısının **mesajına** kondu (namespace apply'ı artık düşerse
> `01-rbac.yaml`'ı adıyla söylüyor). ⚠️ Kural değiştirmek deploy'un yetkisinde
> **değil** ve öyle kalıyor: bu dosya **operatörün**, `tappa-secrets` gibi.
>
> **İŞ 2 — T43 KÖKÜNDEN KALKTI: DOCKER HUB, PUBLIC.**
> FAZ C'nin **ayırt edici kontrolü** zaten cevaptı: **public** imaj + `imagePullPolicy:
> Never` → **düğüm önbelleğinden açıldı, exit 0**, oysa private imaj kimlik yokken
> **401**. Public depoda **kaydedilecek kimlik yoktur**, yani KEP-2535 kusuru tanım
> gereği oluşamaz. Kazanılanlar tek tek: *"Write the GHCR pull credential"* adımı
> **tamamen kalktı** (bir Secret yaratma yolu daha az, §4.7) · iki pod spec'inden
> `imagePullSecrets` **kalktı** (render edilmiş spec'lerde alan **ABSENT**) ·
> `secret/ghcr` yok → bayat kimlik penceresi yok, `kubectl rollout undo` **401
> almıyor** · `permissions: packages: write` **kalktı** · `:main` etiketi artık
> **build de push da edilmiyor** (hiçbir şey referans etmiyordu; public bir registry'de
> hareketli etiket, reponun kendi adını koyduğu kusur sınıfı).
> **Takılmış-pod kurtarma adımı: ÖLÇÜLDÜ, GEREKMİYOR, KALDIRILDI.** Adımın yazılı
> öncülü *"sır tazelendi ama kubelet'in geri çekilme aralığı büyüdü"*ydü; sır yoksa o
> pencere de yok. Kalan `ImagePullBackOff` sebepleri (depo private kalmış · `429` ·
> etiket yok) bu adımla **düzelmiyor** ve zaten adım `apply`'lardan **önce** koştuğu
> için yalnız **önceki** deploy'un pod'larını görüyordu — yeni sha zaten yeni bir
> ReplicaSet ve yeni bir pod demek. Premisi yanlış olan **yıkıcı** bir adımı tutmak
> kartın imza kusuru olurdu. Bununla birlikte README sınırı
> **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** de kalktı.
> **`scripts/verify-deployment.sh pull-credential` de kaldırıldı** — konusu var olmayan
> bir Secret'tı, yani her sağlıklı kümede *"UNREADABLE"* basacaktı: var olmayan bir
> arıza hakkında alarm. `cloudflare` ve `db-role` modlarının sözleşmeleri
> **değişmedi**.
> ⚠️ **YERİNE GEÇEN SAYILMIŞ SINIR:** Docker Hub anonim çekme bütçesi
> (`x-ratelimit-limit: 100;w=3600`, IP başına) artık **ürün imajlarını da** kapsıyor →
> README sınır **[Docker Hub anonim çekme bütçesi]**.
>
> **🔴 BU BLOK ÜÇ CÜMLEYİ ADIYLA İPTAL EDİYOR** (kartın iki yerinde çelişik metin
> kalmasın):
> 1. FAZ C'nin *"Hafifletme (çare değil): `deploy.yml`'e **Recover pods stuck on a
>    stale pull credential** adımı"* cümlesi — **o adım artık YOK**. FAZ C'de doğruydu
>    ve orada doğru kalıyor; bugünkü dosyayı tarif etmiyor.
> 2. FAZ C'nin *"`verify-deployment.sh pull-credential` iki damgayı kanıt olarak
>    basıyor"* cümlesi — **o mod artık YOK**.
> 3. T43'ün *"kalıcı çare hâlâ kullanıcının: paketleri public yap ya da `read:packages`
>    PAT"* cümlesi — **çare uygulandı, ama GHCR'da değil**: GHCR görünürlüğünün API'si
>    yok (üç kez ölçüldü: `PATCH /user/packages/container/tappa` → **404**;
>    `gh api /user/packages/container/tappa` → **403**) ve kullanıcı arayüzden iki kez
>    denedi. Kullanıcı kararı **Docker Hub public** oldu; AWS ECR elendi (private ECR
>    aynı kimlik sorununu dönen bir IAM token'ıyla geri getirir, ECR Public yalnız
>    `us-east-1`'den push kabul eder ve bir AWS hesabı ister).
> 🔴 **EMEKLİ EDİLEN İKİ SINIR ANAHTARI — grep'in bir yere düşmesi için burada.**
> `deploy/README.md`'nin sınır listesinde artık **YOK**: **[GHCR çekme kimliği
> ömürlü]** (yerine → **[Docker Hub anonim çekme bütçesi]**, aynı 12. sırada) ve
> **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** (tarif ettiği adım silindi;
> madde kaldırıldı, liste `1..17` ardışık kaldı). Bu defterin **yukarıdaki FAZ C
> blokları** ikisini de hâlâ adıyla anıyor ve **öyle kalıyor** — onlar tarihli
> kayıtlar ve yazıldıkları gün doğruydular; bugünkü listeyi tarif etmiyorlar. Yeni
> eklenenler: **[deploy kimliği namespace'in sırlarını okuyabilir]** (16),
> **[`kubectl auth can-i` alt kaynakları GÖRMÜYOR]** (17); 8. madde
> **[kubeconfig'in kimliği doğrulanmadı]** adını kazandı.
>
> **DEĞİŞMEYENLER, açıkça:** ölçülmüş karar #2 (**migration `Job`, initContainer
> değil**) ayakta — `DATABASE_MIGRATE_URL` uygulama pod'unun spec'inde **yok**
> (render edilmiş spec'te env anahtarları: `DATABASE_URL`, `TAPPA_SESSION_HMAC_KEY`,
> `TAPPA_TAG_KEK`, `TAPPA_INVITE_HMAC_KEY`), sır **anahtar anahtar** veriliyor
> (`envFrom: secretRef` **yok**); değişmez `sha-<12hex>` etiketi **tek** etiket;
> `verify-image.sh`'in **iki kapısı da** push'tan **önce** çağrılıyor (adım 7, 8 →
> adım 9); `wait-for-postgres` initContainer'ları ve `12-networkpolicy.yaml`
> **aynen** duruyor (`git diff --stat deploy/k8s/12-networkpolicy.yaml` → boş).
>
> 🔴 **KULLANICI EYLEMİ GEREKİYOR — üç madde, deploy bunlarsız koşmaz:**
> **(a)** `deploy/k8s/00-namespace.yaml` + `deploy/k8s/01-rbac.yaml`'ı **cluster-admin**
> kubeconfig ile bir kez `apply` et (deploy kendi rolünü değiştiremez — ve bu doğru).
> **(b)** GitHub deposuna **`DOCKERHUB_USERNAME`** ve **`DOCKERHUB_TOKEN`** secret'ları
> (Docker Hub → Account Settings → Personal access tokens, **Read & Write**). Ajan
> bunları **oluşturamaz** ve denemedi; bugün depoda **yalnız `KUBE_CONFIG`** var
> (`gh secret list` ile ölçüldü).
> **(c)** İlk push'tan sonra `atknatk/tappa` ve `atknatk/tappa-migrate` depolarını
> Docker Hub'da **Public** yap — ilk push depoyu **private** yaratır ve private kalırsa
> KEP-2535 kusuru olduğu gibi geri gelir.
> ⚠️ **Secret yokluğu yolu ölçüldü:** tanımsız bir GitHub secret'ı **boş dizeye**
> dönüşür, hata vermez; `docker login` o değerle **rc=1** ve `username is empty` /
> `password is empty` diyor — hangi secret'ın eksik olduğunu da nereden alınacağını da
> **söylemiyor**. Bu yüzden iş akışının **birinci** adımı ikisini de sayıyor ve eksikse
> **hiçbir şey inşa etmeden** duruyor. Sekiz dejenere girdi koşuldu (ikisi de tanımsız ·
> ikisi de boş · yalnız biri · **tek boşluk** · **sekme+satırsonu** · sondaki satırsonu ·
> ikisi de dolu) → **altısı exit 1 ve adıyla mesaj, ikisi exit 0**.
>
> **🔴 [FAZ D · 2026-08-16] 2. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BİR SAYI YANLIŞTI, VE ASIL
> İŞ BİR GARANTİYE MEKANİZMA VERMEKTİ.**
>
> **B1 (bloklayan) — `deploy/README.md`'de ÖLÇÜLMÜŞ BİR SAYI YANLIŞTI, ve aynı
> cümlede ikinci kez.** *"14 nesne … ve **beşi küme kapsamlı RBAC**"* yazmıştım.
> `kubectl api-resources`'ın kendi kapsam alanıyla ölçüldü: **14 nesnenin 3'ü** küme
> kapsamlı (`Namespace/tappa`, `ClusterRole/tappa-namespace-only`,
> `ClusterRoleBinding/tappa-namespace-only`); `01-rbac.yaml`'ın getirdiği **beş**
> nesnenin **yalnız ikisi** küme kapsamlı, **dördü** `rbac.authorization.k8s.io/v1`
> grubunda ve `ServiceAccount` düz `v1`. *"Beş yeni nesne"* ile *"beş küme-kapsamlı
> nesne"* birbirine karışmıştı. ⚠️ **Kusurun ağırlığı sayıda değil yerinde:** o
> paragrafın **var olma sebebi** bir önceki yanlış sayıydı (2026-08-15 denetimi), yani
> aynı hata aynı paragrafta tekrarlandı. Düzeltildi ve gerekçesi paragrafın içine
> ölçümüyle yazıldı.
>
> **B2 — MEKANİZMASI OLMAYAN GARANTİYE MEKANİZMA VERİLDİ: yeni kapı,
> *"Gate — both images must be pullable with NO credential"*.** Denetçi ölçtü:
> `hub.docker.com/v2/repositories/atknatk/tappa/` → **404**, `…/tappa-migrate/` →
> **404** — **iki depo da yok**, yani ilk push ikisini de **private** yaratacak ve
> KEP-2535 kusuru aynen geri gelecekti. Bugünkü hâl **fail-closed**'du (migration
> pod'u `ImagePullBackOff`'a düşer, `.status.failed` artmaz, anket 600 sn'yi harcar,
> `rollout`'a hiç varılmaz) ama benim raporumda *"önce Public yarat"* diye
> **kullanıcıya talimat** olarak duruyordu — kartın imza kusuru. Artık kapı: push'tan
> hemen sonra, `kubectl` kurulmadan **önce**, iki imaj da **kimliksiz** çekilebiliyor
> mu diye bakılıyor.
> **Anonimlik `docker logout` ile DEĞİL, `DOCKER_CONFIG`'i boş bir dizine çevirerek
> sağlandı** — ve gerekçesi ölçüm: bu makinede `~/.docker/config.json` →
> `credsStore: desktop`, yani `logout` bir *credential helper* üzerinden gider ve
> runner'daki düz dosya davranışını **temsil etmez**. **Ayırt edici kontrol:** boş
> `DOCKER_CONFIG` + `library/postgres:17-alpine` → **rc 0**; **uydurma** bir docker.io
> kimliği taşıyan `DOCKER_CONFIG` + aynı imaj → **rc 1, `unauthorized: incorrect
> username or password`**. İkincisi olmasa birincinin anonim olduğu kanıtlanamazdı.
> Kapının docker'a verdiği dizin doğrudan basıldı: `entries=[]`.
> **Dejenere yol sayımı — 10 hücre, YALNIZ BİRİ GEÇİYOR:** depo yok ×2 (bugünkü
> gerçek hedefler) → **rc 1, 2 hata** · public depo/etiket yok → **rc 1, 2** · registry
> ulaşılamıyor (DNS) → **rc 1, 2** · `docker` ikilisi yok (coreutils var) → **rc 1, 2**
> · **ikisi de çekilebiliyor → rc 0, 2 ok** · biri OK biri `denied` → **rc 1, 1 hata +
> 1 ok** (atıf doğru) · `429 toomanyrequests` → **rc 1, 2** · sessizce düşüyor → **rc
> 1, 2** · **çıkış 0 ama HİÇ çıktı yok → rc 1, 2** · çıkış 0 ama manifest olmayan
> çıktı → **rc 1, 2**. Yani **1 geçer, 9 fail-closed, 0 fail-open**.
> 🔴 **VE SONDAN İKİNCİ HÜCRE İLK TASLAKTA FAIL-OPEN'DI — kendi matrisim yakaladı.**
> Kapı yalnız çıkış koduna bakıyordu; sıfır dönüp **hiçbir şey basmayan** bir istemci
> *"anonymous pull OK"* diye geçiyordu: reponun adını koyduğu tuzağın en saf hâli
> (*sağlıklı cevap ile BOŞ cevap aynı şey değildir*). Kapı artık **kanıta** bakıyor —
> çıktı `"schemaVersion"` taşımak zorunda; üç public referansta ölçüldü
> (`postgres:17-alpine`, `alpine:3`, `hello-world:latest` → rc 0, 5000–6339 bayt, üçünde
> de var). Taşımazsa **fail-closed**.
> ⚠️ **Bu turda kendi ölçüm harnessimde iki hata yaptım, ikisi de deftere:**
> (a) ilk matriste *"her ikisi de PUBLIC"* diye etiketlediğim hücre aslında *"public
> depo, etiket yok"*tu — kapı `"$IMAGE:sha-$SHORT"` kuruyor ve hiçbir public deponun
> `sha-x` diye bir etiketi yok, yani **yanlış hücreyi ölçüp doğru sandım**; stub'la
> ayrıca koşuldu. (b) *"docker ikilisi yok"*u `PATH=/nonexistent` ile denedim ve
> `mktemp` de kayboldu, yani ölçtüğüm şey docker'ın yokluğu değil coreutils'in
> yokluğuydu (rc 127, çıktı yok); coreutils'i sembolik bağlarla koruyan bir PATH ile
> yeniden koşuldu → **rc 1, 2 adlandırılmış hata**.
> (c) 🔴 **VE ÜÇÜNCÜSÜ SINIFIN TAM ORTASINDAN: sarkan-atıf TARAYICIM FAIL-OPEN'DI.**
> Üç alternatifli bir regex yazıp `k = m.group(1) or m.group(2)` diye okuyordum ve
> `if not k: continue` ile devam ediyordum — yani **üçüncü** gruba düşen her eşleşme
> sessizce atlanıyordu. Tarayıcı *"sarkan atıf = 0"* diyordu; aynı dosyayı tek
> parça okuyan düzeltilmiş sürüm `deploy.yml`'in hata mesajındaki
> **ASCII'ye indirgenmiş** bir sınır anahtarını (`[Docker Hub anonim cekme butcesi]`,
> README'de `[Docker Hub anonim çekme bütçesi]`) **hemen** buldu — yani operatörün
> grep'leyip **hiç bulamayacağı** bir atıf. Anahtar mesajdan çıkarıldı (CLAUDE.md §7:
> hata mesajları ASCII İngilizce; bölüm **adıyla** gösteriliyor) ve `Dockerfile`'daki
> atıf README anahtarına **byte-byte eşit** olduğu doğrulandı. **Ders, bu turun kendi
> cümlesiyle: bir doğrulama aracı da bir kod yoludur ve dejenere girdisi vardır —
> benimkinin dejenere girdisi kendi regex'imin üçüncü grubuydu.**
>
> **U1 — `batch/jobs`'ta `watch` YOK, bugün doğru, ve artık YAZILI.** Denetçinin
> uyarısı kendi ölçümümle yeniden üretildi, iki dalın ikisi de: finalizer **yokken**
> (bugünkü hâl) `kubectl delete job --wait=true -v=8` → `DELETE`, sonra `GET .../jobs/
> tappa-migrate` → **404**, `watch=true` isteği **0**; kum havuzunda Job'a bir
> finalizer takıp aynı komut → `DELETE`, `GET` (nesne duruyor), sonra
> `GET .../jobs?fieldSelector=metadata.name%3Dtappa-migrate&…&watch=true`,
> `watch=true` isteği **1**. Canlı Job `{.metadata.finalizers}` → **boş**. Yani ikinci
> dal bugün oluşmuyor; oluşursa **Forbidden → adım kırmızı → migration'sız şemaya
> ikili sevk edilmez**, yani fail-closed. Kural yorumu bu ölçümle genişletildi;
> **fiil eklenmedi**, çünkü kimsenin görmediği bir vaka için verilen yetki kimsenin
> denetlemediği yetkidir.
>
> **U2 — `tr -d '[:space:]'` BAYT YÖNELİMLİYDİ, VE YEREL ÖLÇÜMÜM RUNNER'I TEMSİL
> ETMİYORDU.** Denetçi ölçtü: yalnız **U+00A0** (`c2 a0`) ya da **U+3000**
> (`e3 80 80`) içeren bir değer GNU coreutils'te (yani `ubuntu-latest`'te) kırpılmadan
> **geçiyor**. Bu makinede BSD `tr` ikisini de kırpıyor, yani **benim yerel ölçümüm
> tam tersini söylüyordu** — ve `gtr` bu makinede kurulu değil, yani runner'ın davranışı
> burada **hiç** ölçülemezdi. Sonucu ağır değildi (bir adım sonra `docker login`
> düşer, fail-closed) ama adımın **var olma sebebini** — eksik secret'ı **adıyla**
> söylemek — o girdide karşılamıyordu.
> **Çözüm aracı değiştirmek değil, testi ÇEVİRMEK oldu:** *"yalnız boşluk mu"* yerine
> *"kullanılabilir bir karakter içeriyor mu"* — `case $u in *[0-9A-Za-z]*)`. Harici
> araç yok, yani yerel/runner ayrımı da yok. Beş locale'de ölçüldü (`C`, `C.UTF-8`,
> `en_US.UTF-8`, **`tr_TR.UTF-8`**, `POSIX`): U+00A0 ve U+3000 **beşinde de REDDEDİLDİ**,
> `alice` beşinde de kabul; Türkçe locale'in noktasız-i tuzağı ayrıca sınandı
> (`ıstanbul9` → kabul) ve yalnız rakamdan oluşan `12345` → kabul. Geçerli hiçbir
> girdi elenmiyor: Docker Hub kullanıcı adı `[a-z0-9]`, PAT `dckr_pat_<base64url>`.
> **16 hücre koşuldu → 11 fail-closed (hepsi eksik secret'ı adıyla söylüyor), 5 geçiş**
> (hepsinde gerçek bir alfanümerik var), ve değer **hiçbir yolda echo edilmiyor**
> (`::error::PWNED-MARKER` girdisi → çıktıda **0** eşleşme; yalnız `${#u}` basılıyor).
>
> **🔴 [FAZ D · 2026-08-16] 3. TUR — `tappa-security-auditor` RED. İKİ BLOKLAYANIN
> İKİSİ DE §4.6'NIN ALTYAPI YÜZÜ, VE İKİSİ DE BU TURDA BENİM YAZDIĞIM METİNDEYDİ.**
> Kod değil, **runbook**. Ders keskin: *"ürün temiz"* ile *"runbook operatörü doğru
> yere gönderiyor"* ayrı sorulardır ve ikincisi de bloklayabilir.
>
> **B1 — REÇETE EDİLEN EYLEM GARANTİLİ BAŞARISIZDI.** Baştan yazdığım *"Olay
> müdahalesi §2"*, üç satırlık tablodan sonra **"Çare (her üçünde de) yeni bir deploy
> koşusudur"* diyordu — ve bu, tablonun **kendi çare sütunuyla iki satırda
> çelişiyordu**. Ölçüldü: depo private iken yeni koşu, bu turda **benim eklediğim
> kapıda** ölür — kapı `deploy.yml`'de **10. adım**, `Install kubectl` **11. adım**,
> yani `kubectl` runner'a daha kurulmamıştır; kapı rc 1 verir ve *"Nothing has been
> applied to the cluster"* der. Yani operatörün yaptığı tek şey kümeye dokunmadan
> durmak olur ve **uygulama pod'u açılmamaya devam eder**. `429` satırında da
> geçersiz: kapı runner'ın adresinden geçse bile çeken taraf **düğüm**dür.
> 🔴 **Neden §4.6:** bu bölümün müdahale ettiği hâl pod'un **hiç açılmadığı** hâldir —
> 04:00 vardiyası tap sayfasını yükleyemez ve **kaydedilemeyen dokunuş kaybolan
> kayıttır**. Genel cümle **kaldırıldı**; tablo başlığı artık *"satır bazında; genel
> bir çare YOK"* diyor ve her satır kendi doğru sırasını taşıyor (*"önce Public,
> SONRA yeni koşu"* · *"bekle, yeni koşu düzeltmez"* · *"yeni koşu"*).
>
> **B2 — AYNI DOSYA KENDİNİ YALANLIYORDU.** Birinci satırın yanına *"bunu kümede
> görüyorsan iş akışının kapısı atlanmıştır … buraya ancak **elle `apply` edilmiş**
> bir manifestle gelinir"* yazmıştım. **Yanlış**, ve çürüten cümle **aynı dosyanın
> sınır 12'sinde** duruyordu (*"kapı bir depoyu sonradan private'a çeviren kimseyi
> engelleyemez"*). Elle apply olmadan dört adımda üretilir ve **dördü de sınır 12'de
> sayılı**: deploy N yeşil → depo sonradan private → düğüm imajı düşürür
> (`imageMinimumGCAge: 2m0s`, `crictl rmi`, düğüm yeniden kurulumu) → pod yeniden
> başlar. Operatör **hiç olmamış** bir olayı ararken pod kapalı kalırdı. Cümle
> *"kapı bir **deploy anı** ölçümüdür, süregelen bir garanti değil"* ile değiştirildi.
>
> **U1 — 18. SINIR: sevk edilen commit'in kimliği herkese açık (§4.7).** Liste 1..17
> ardışıktı ama **hiçbir maddesi ürün ikililerinin herkese açık olmasını
> adlandırmıyordu**; 12. madde yalnız çekme bütçesini sayıyordu. Denetçinin ölçtüğü
> gibi bedelin **büyük kısmı sıfır** ve bunu kendi ölçümümle doğruladım: depo zaten
> **PUBLIC** (`gh repo view` → `PUBLIC`), imaj içerikleri `Dockerfile`'ın son iki
> aşamasından birebir okunuyor — uygulama = CA paketi + `/tappa`; migration = CA
> paketi + `/goose` + `/migrations` (**20 `.sql`**, `find db/migrations -name '*.sql'`
> ile sayıldı, hepsi zaten public depoda). 🔴 **Ama sayılmayan gerçek bir delta var
> ve kontrol ölçümüyle doğrulandı:** public bir Docker Hub deposunun etiket listesi
> **kimliksiz okunuyor** (`library/alpine`: anonim token → `/v2/…/tags/list`
> **HTTP 200**; `hub.docker.com/v2/repositories/library/alpine/tags` → etiket adları
> düz döndü). Etiketler `sha-<12hex>` olduğu için **üretimdeki commit ve bir
> düzeltmenin sevk edilip edilmediği dışarıdan okunabilir**. Bunu başka hiçbir yüzey
> vermiyor — ölçüldü: `grep -rn buildinfo internal/handler web/templates` → **0**;
> `buildinfo` yalnız `cmd/tappa/main.go:83`'te log'a yazılıyor. Yani M8-01'in
> *"derleme kimliği herkese açık bir uç noktaya çıkmıyor"* kararı **yeni bir
> kanaldan** deliniyor. 18. madde yazıldı (ne açılıyor · ne açılmıyor, ölçülmüş dosya
> listesiyle · neden kabul edildi · kapatmanın bedeli). Ayrıca operatör adımı 4'ün
> *"imaj `scratch` + iki dosya"* cümlesi **iki artefaktın yalnız birini** anlatıyordu;
> düzeltildi.
>
> **U2 — DOSYADAKİ SON SATIR-İÇİ SIR GENİŞLETMESİ KALKTI (§4.7).**
> `echo "${{ secrets.KUBE_CONFIG }}" | base64 -d` — bir `run:` gövdesindeki ifade
> kabuk başlamadan **önce** yerine konur, yani runner diskindeki render edilmiş adım
> betiğinde **düz metin** durur. Bu, dosyanın **en değerli** kimliğiydi (sınır 8'e
> göre hâlâ cluster-admin olabilir) ve bu turda **benim eklediğim iki adım** zaten
> doğrusunu yapıyordu. Artık `env: KUBE_CONFIG_B64` + `printf '%s' "$KUBE_CONFIG_B64"`
> (`echo` değil: başında `-` olan ya da ters bölü taşıyan bir değerde davranışı
> kabuğa bağlı). Ölçüm: `grep -n 'secrets\.'` → **5 eşleşme, beşi de `env:`
> bloğunda, `run:` gövdesinde 0**. Sızmadığı da ölçüldü: her `run:` gövdesi
> ayrıştırılıp yorumlar atılarak → **17 gövde, yorum-dışı `set -x` sayısı 0**.
> ⚠️ Ve bozuk bir secret'ın nerede yakalandığı ölçüldü: `base64 -d` **hoşgörülü**
> (boş değer → 0 baytlık dosya, exit 0; base64 olmayan değer → 6 bayt çöp, exit 0);
> yakalayan şey adımın **son satırı**: `kubectl config current-context` → ikisinde de
> **exit 1**, kümeye hiçbir istek gitmeden.
>
> **🔴 BU TURDA KENDİ DOĞRULAMA ARAÇLARIM İKİ KEZ DAHA YANLIŞ SAYDI — sınıfın
> yedinci ve sekizinci örneği, ikisi de kendi ölçüm harness'imde.**
> (a) *"secrets satır-içi mi"* sınıflandırıcım `[A-Z_]+` yazıyordu ve **yeni değişken
> `KUBE_CONFIG_B64` rakam içerdiği için** `env:` bloğundaki satırı *"INLINE"* saydı —
> yani düzelttiğim şeyi düzeltilmemiş gösterdi. `[A-Z0-9_]+` ile yeniden koşuldu:
> **5 env: / 0 inline**. (b) *"`set -x` bu dosyada hiç geçmiyor"* diye yazdığım
> cümleyi `grep -c 'set -x'` **1** ile yalanladı — tek eşleşme **cümlenin kendisiydi**.
> İddia ölçülebilir hâle çevrildi: yorumlar atılarak, `run:` gövdelerinde **0**.
> **Ders, 2. turun (c) maddesinin devamı: bir doğrulama aracı da bir kod yoludur; ve
> bir iddiayı ölçülebilir yazmazsan, kendi metnin onu yalanlayabilir.**
>
> **🔴 [FAZ D · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED, ÜÇ BLOKLAYAN. VE BULGU
> SINIFIN DOKUZUNCU ÖRNEĞİ, YENİ BİR YERDE: AĞACI ÖLÇTÜM, RUNBOOK'UN KONUSU OLAN
> KÜMEYİ HİÇ ÖLÇMEDİM.** Dört tur boyunca `git diff`, `bash -n`, `--dry-run=client`
> ve stub matrisleri koşturdum; **`kubectl -n tappa get deployment -o jsonpath` bir
> kez bile koşmadı**. Runbook'un doğruluğu ağaca değil **kümeye** göredir.
>
> **KÜMENİN BUGÜNKÜ HÂLİ (kendi ölçümüm, salt okuma, 2026-08-16):**
> `secret/ghcr` **VAR** (18 sa, `dockerconfigjson`) · `deployment/tappa` →
> `ghcr.io/atknatk/tappa:sha-353897c6d5f6`, `imagePullSecrets: ghcr` · **üç
> ReplicaSet'in üçü de** `ghcr.io/...` + `ps=ghcr` · Job → `ghcr.io/atknatk/
> tappa-migrate:...` + `ps=ghcr` · `gh secret list` → **yalnız `KUBE_CONFIG`** ·
> `hub.docker.com/v2/repositories/atknatk/{tappa,tappa-migrate}/` → **404, 404**.
> Yani **ağaç Docker Hub'ı tarif ediyor, küme GHCR koşuyor**, ve operatör adımı 4
> yapılmadan hiçbir Docker Hub deploy'u koşamaz (1. adımda durur). ✅ Yan bulgu:
> FAZ C **inmiş** — Deployment ve Job'ın ikisi de `wait-for-postgres` taşıyor.
>
> **B1 — RUNBOOK BİR DURUMU ŞİMDİKİ ZAMANDA ANLATIYORDU VE BİR KOMUTU ÜRÜNÜ
> DÜŞÜRÜRDÜ.** *"Elle deploy / rollback"*taki
> `set image ... tappa=docker.io/atknatk/tappa:sha-<12hex>` bugün koşulsa **çekilemeyen
> bir imajı** `1/1 Running` bir pod'un yerine koyardı → `ImagePullBackOff`. **3. turun
> RED'i *garantili başarısız* içindi; bu *garantili zararlı*.** Ve aynı kökten dört
> yanlış cümle daha: *"`rollout undo` artık ayrıcalıklı risk taşımıyor"* (üç RS de
> `ghcr` + `ps=ghcr` → tam da kaldırdığım 401 senaryosu) · *"'ayağa kalkabilir miyim'
> sorusu ARTIK YOK"* (oluşabilir, ve onu ölçen `pull-credential` **silindi**) ·
> *"`secret/ghcr` arayan biri var olmayan bir nesneyi arar"* (**var**) · sınır 12'nin
> *"kaldırıldı"*sı (ağaçta evet, kümede hayır).
> 🔴 **Ve dosya doğru kalıbı ZATEN BİLİYORDU:** bölüm 1'de *"Ağaçtaki manifestler
> taşıyor; KÜMEDEKİ nesneler henüz taşımıyor"* + doğrulama komutu duruyor. Aynı kalıp
> şimdi **dört yere daha** kondu: rollback bölümünün başına (ön koşul + ölçüm komutu,
> ve komuttaki registry `<registry>` ile parametrik hâle geldi), olay önsözüne, bölüm
> 2'ye ve sınır 12'ye. `rollout undo` uyarısı **geri getirildi** ve hangi koşulda
> düşeceği yazıldı.
>
> **B2 — BÖLÜM 2'NİN BAŞLIĞI KENDİ BÖLÜMÜNÜ YALANLIYORDU.** *"artık kimlik sorunu
> DEĞİL, **iki başka sebep**"* — tablo **üç** satır taşıyordu ve bölümün kendi metni
> *"üç satırda da"* diyordu; üstelik kimlik sorunu bu kümede **hâlâ canlı**. Başlık
> *"dört sebep, ve hangisinin geçerli olduğu KÜMEYE bağlı"* oldu ve **dördüncü satır**
> eklendi (bugünkü GHCR 401'i, 2026-08-15'te **4 dk 39 sn** olarak ölçülmüştü).
> 🔴 **Kendi hatam, sınıfın onuncu örneği:** 9. iddiamda *"1 başlık, 1 ayraç, 3 veri
> satırı"* diye **doğru** saydım — ama sayımı **hiç başlığın kendisiyle
> karşılaştırmadım**. Tarayıcı bir önceki kusurun şekline (fazladan başlık satırı)
> göre yazılmıştı; o turun ürettiği şekle (başlıktaki sayı sözcüğü) göre değil.
>
> **B3 — SEVK EDİLEN BİR YORUMDA *ÖLÇÜM OLARAK DURAN* YANLIŞ BİR OLGU, VE ÖLÇÜM YİNE
> YEREL ARAÇLA YAPILMIŞTI.** `deploy.yml`'in kubeconfig adımı *"`base64 -d` lenient,
> gerçek kapı son satır"* diyordu. **Runner'da ölçüldü** (`docker run --rm
> ubuntu:24.04`, GNU coreutils **9.4**) ve yerelle yan yana kondu:
>
> | girdi | ubuntu:24.04 (GNU 9.4) | yerel BSD |
> |---|---|---|
> | boş | rc 0, 0 bayt | rc 0, 0 bayt |
> | base64 olmayan | **rc 1, `base64: invalid input`** | rc 0, 6 bayt |
> | yalnız boşluk | **rc 1** | rc 0, 0 bayt |
> | `-` ile başlayan | **rc 1** | rc 0, 3 bayt |
> | geçerli | rc 0, 15 bayt | rc 0, 15 bayt |
>
> Yani runner'da `base64 -d` **hoşgörülü değil**, *"6 bayt çöp"* **oluşmuyor**, ve son
> satır dört bozuk girdiden **yalnız birinin** (boş) kapısı. Yön hâlâ fail-closed, yani
> güvenlik değil **doğruluk ve teşhis** kusuru: yorum bir bakımcıya *"son satır
> gereksiz, silinebilir"* diye **yanlış bir nedensellik modeli** veriyordu — oysa boş
> girdiyi yakalayan **yalnız o satır**. 🔴 **Ağırlığı yerinde: bu, aynı dosyanın 118-128.
> satırlarında ADIYLA anılan kusurun (`tr -d '[:space:]'`, yerel-aracı-runner-aracı
> yerine koymak) AYNI TURDA, üç paragraf aşağıda tekrarı.** §4.7 temiz: GNU `base64`
> yalnız `base64: invalid input` basıyor, değeri basmıyor (markör sondası → **0**).
>
> **U1** `429` satırı Deployment pod'u için doğruydu ama **migration Job'ını
> kurtarmıyor**: `activeDeadlineSeconds: 600` + `backoffLimit: 2`, saatlik pencereden
> çok önce kalıcı `Failed` → yine yeni bir koşu gerekir. Satıra yazıldı.
> **U2** *"SADECE `ImagePullBackOff` olanı sil"* **pod tipini ayırmıyordu**; üç satırlık
> bir tablo eklendi: Deployment pod'u **zararsız** · **Job pod'u** `.status.failed`'ı
> artırır ve `backoffLimit: 2` ile **üç silme kalıcı `Failed`** yapar ·
> **`tappa-postgres-0`** *veritabanını yeniden başlatır*.
> **U3** olay önsözünün *"her satır bu kümede fiilen ölçüldü"* garantisi, altına
> **ölçülmemiş** Docker Hub satırları eklendiği için fazla iddia hâline gelmişti —
> cümle daraltıldı ve ölçülmemiş satırlar **satırında işaretlendi** (B1'le aynı kök:
> değişmemiş bir garanti cümlesinin altına yeni satır eklemek).
> **U4** *"`deploy.yml`'in on beş küme çağrısı"* yanlıştı: **15** benim prova betiğimin
> **adım numarasıydı**. Yeniden sayıldı, yöntemi yazıldı (yorumlar atılarak, API'ye
> giden `kubectl` çağrıları; `version --client`, `config current-context` ve
> `--dry-run=client` hariç, `db-role`'un `exec`'i dahil): **19** — adım 14→2 · 15→5 ·
> 16→5 · 17→3 · 18→1 · 19→3, denetçinin sayısıyla **birebir**.
> **U5** `dnscheck` sondası `postgres:17-alpine` istiyor, yani `429` olayında ters
> tepebilir; bir cümlelik uyarı eklendi.
>
> **🔴 [FAZ D · 2026-08-16] 7. TUR — YENİ ÜÇÜNCÜ GÖZ RED, TEK BLOKLAYAN, SINIFIN
> ON BİRİNCİ ÖRNEĞİ: DÜZELTMEYİ README'YE UYGULADIM, AYNI TURDA YENİDEN YAZDIĞIM
> DİĞER ÜÇ DOSYAYA UYGULAMADIM.** 5. turda kümeyi ölçüp README'yi gerçeğe eşitledim;
> ama `20-app.yaml`, `30-migrate-job.yaml` ve `deploy.yml`'de **aynı commit'te
> eklediğim** dört cümle, README'nin **adıyla geri çektiği** cümlelerdi. Repo aynı
> commit'te hem geri çekmeyi hem geri çekilen cümlenin iki kopyasını taşıyordu, ve
> bağ kopmuştu: sildiğim eski yorum *"deploy/README.md carries the warning in BOTH
> places"* diye README'ye işaret ediyordu, yenisinde o işaret **yoktu** — oysa README
> sınır 5 ve 12 okuru açıkça `20-app.yaml`'a **gönderiyor**. Olay anında oraya bakan
> operatör *"undo artık 401 almıyor"* okuyup `rollout undo` koşar ve **şu an
> `1/1 Running` olan üretim pod'unu** düşürürdü.
>
> 🔴 **VE ÇARE ÜÇ DOSYAYA DAHA UYARI EKLEMEK DEĞİLDİ — İDDİANIN BİÇİMİYDİ.** Sınıf on
> bir kez tekrarladı ve her seferinde çare *"bir yer daha düzelt"* oldu; FAZ C'yi kıran
> şey buydu ve burada da o geçerli: **savunulan bir cümleyi, bayatlayamayacak bir
> cümleyle değiştir.** *"The repository **is** public"* dünya hakkında bir iddiadır ve
> bayatlar; *"**WHILE** the image was pulled with a credential, X; **ONCE** it is
> pulled anonymously, Y"* mekanizma hakkındadır ve bayatlayamaz. Üç dosyadaki dört
> cümle **koşullu** hâle getirildi, hepsi *"bugünkü durumun tek kaydı
> `deploy/README.md` → Elle deploy / rollback"* diye **tek yere** işaret ediyor (eski
> yorumun yaptığı gibi). ⚠️ **Ve bu iddia gerçeğe indiriliyor:** *"bir sonraki geçişte
> tek bir yer güncellenecek"* fazla iddiaydı — ölçüldü, bugünkü küme durumu
> `deploy/README.md` içinde **altı ayrı bölgede** tekrarlıyor. Doğru cümle: **tek
> DOSYA, altı bölge, ve altısının da kendi düşme koşulu yazılı** — yani hiçbiri
> sessizce bayatlayamaz.
> **Ölçüm — istenen hedef ve sonucu:** `deploy/` + `.github/` altındaki *bugünkü küme
> hakkında şimdiki-zaman iddiası* sayısı → **0**. Kalan dört eşleşmenin hepsi elle
> tasnif edildi: 2 **koşullu** (WHILE/ONCE) · 1 **bu dosya hakkında** (*"a pod born
> from THIS spec"*) · 1 **genel mekanizma** (*"a public repository … by definition"*).
>
> **🔴 VE DETEKTÖRÜM İKİ KEZ YANILDI — sınıfın on ikinci ve on üçüncü örneği, ikisi de
> benim doğrulama aracımda, ikisi de kendi matrisimde yakalandı.**
> (a) İlk sürüm niteleyiciyi **±2 satırlık komşulukta** aradı; `30-migrate-job.yaml`'da
> üç satır ötedeki alakasız bir *"used to"*, çıplak bir dünya iddiasını **temize
> çıkardı** — yani detektör *"0 çıplak iddia"* dedi, oysa vardı. Cümle kapsamına
> daraltıldı ve iddia gerçekten koşullu yazıldı.
> (b) İkinci sürüm cümleyi `;` ve `:`'de de böldü, böylece *"ONCE …:"* koşulu
> **kendi sonucundan koparıldı** ve yönetilen bir yan cümle **çıplak** göründü. Tam
> nokta ile bölmeye çevrildi. **Ders (2. ve 3. turun devamı): bir detektörün kapsam
> penceresi de bir dejenere girdidir.**
>
> **VE 5. TURUN İKİ KENDİ HATASI DEFTERE GEÇMEMİŞTİ — buraya yazılıyor** (denetçi
> `grep` ile gösterdi; bu repoda kayıt sohbette değil defterde yaşar):
> (a) bölüm 2'nin satır sayımını ilk yeniden koşuşumda seçici **iki tabloyu birden**
> süpürdü ve `DATA ROWS: 7` dedi (doğrusu: sebep tablosu **4**, pod-tipi tablosu
> **3**); (b) *"canlı prose iddiası"* regex'im `**Dört satırda`yı **büyük harf**
> yüzünden kaçırdı, sonra büyük/küçük harfe duyarsız koşuldu.
> ✅ **VE SINIFI KIRAN ARAÇ DA DEFTERE YAZILIYOR, çünkü kayıtsız bir araç yoktur:**
> 5. turda yazılan **başlık↔tablo kontrolü** — bir başlıktaki sayı sözcüğünü (*"dört
> sebep"*) başlığın yönettiği tablonun **gerçek satır sayısıyla** karşılaştırır,
> bölümdeki her tabloyu ayrı sayar ve düzyazıdaki sayı iddialarını da tarar. Bağımsız
> denetçi onu yeniden koşturdu: **6 tablo**, *"dört sebep"*↔4 · *"Dört satırda"*↔4 ·
> *"üç tipin üçü"*↔3 · *"Üç ayrı taze koşu"*↔3, iki yanlış pozitif (artikel *"bir"*),
> **gerçek uyumsuzluk 0**. Bu kontrol, 4. turda *"1 başlık, 1 ayraç, 3 veri satırı"*
> diye **doğru** sayıp **hiç başlıkla karşılaştırmadığım** kusurun yerine geçiyor.
>
> **N1** `kubectl set image` yer tutucuyu **sessizce kabul ediyor** — çevrimdışı
> ölçüldü (`--local`): çıktı birebir `<registry>/atknatk/tappa:sha-<12hex>`, istemci
> tarafında imaj doğrulaması **yok**. Komut bloğuna yazıldı. **N2** `rollout undo`
> uyarısı kod bloğunun **ardındaydı** ve bloğu toptan kopyalayan onu okumadan koşardı;
> blok **öncesine** bir uyarı ve **üçüncü komutun üstüne** bir işaret kondu. **N4** bir
> sınır anahtarı satır sonunda **bölünmüştü** (`**[Docker Hub anonim çekme\n>
> bütçesi]**`) — render doğru, **arama** değil: altı görünümünün yalnız dördü literal
> `grep`'le bulunuyordu; ikisi birleştirildi → **6/6**. 2. turun ASCII-katlanmış
> anahtar bulgusuyla aynı sınıf, farklı mekanizma. **N5** *"İki tanesi ölçüldü, biri
> yapısal"* **üç** sebep vaat edip **iki** veriyordu → *"İki tane, biri ölçülmüş biri
> yapısal"*.
> ⚠️ Denetçinin eklediği eksik de yazıldı: GNU `base64` hata vermeden **2 bayt kısmi
> çıktı** yazıyor, yani `config` yarım yazılıp adım kırmızıya düşüyor — yön
> fail-closed, sonucu değiştirmiyor.
>
> **🔴 [FAZ D · 2026-08-16] 9. TUR — SON. ÜÇÜNCÜ GÖZ RED, TEK BLOKLAYAN, VE SINIFIN
> ON DÖRDÜNCÜ ÖRNEĞİ: KAPSAMI YİNE BEN SEÇMİŞTİM.**
> Mekanizma her seferinde aynı: **taramayı bir önceki kusurun bulunduğu yere göre
> kapsamlıyorum.** 5. tur: README'yi ölçtüm, **kümeyi** ölçmedim. 7. tur: README'yi
> düzelttim, **aynı turda yeniden yazdığım üç manifesti** düzeltmedim. 9. tur: üç
> manifesti düzelttim, **`scripts/` ve `Dockerfile`'ı** taramadım — üstelik
> `verify-deployment.sh` bu turda **127 satır** kaybetmiş, yani aynı commit'te
> yeniden yazılmış bir dosya.
>
> **BLOKLAYAN — `scripts/verify-deployment.sh:13-18`.** `pull-credential` modunun
> silinme gerekçesi **koşulsuz şimdiki zamanda** yazılmıştı: *"There is no
> `secret/ghcr` any more … nothing records, expires or overwrites a pull credential …
> a diagnostic that says something alarming about a condition that **cannot occur**"*.
> Üç cümlenin üçü de yanlış ve *"cannot occur"* dediği koşul **canlı** (bugün yeniden
> ölçüldü: `secret/ghcr` **var** · `deployment/tappa` → `ghcr.io/…` + `ps=ghcr` ·
> Docker Hub **404, 404**). Kaldırılan teşhisi arayan kişi, **aracın kendi dosyasında**
> *"bu koşul oluşamaz"* okuyacaktı — ve tarif ettiği arıza pod'un **hiç açılamaması**
> (§4.6). Blok WHILE/ONCE biçimine çevrildi ve *"WHILE bir küme hâlâ kimlikle
> çekiyorsa koşul canlıdır; elle okunacak iki komut README'de"* bilgisi eklendi.
>
> **BEŞ DOSYADA ALTI CÜMLE DAHA AYNI BİÇİME ÇEVRİLDİ:** `Dockerfile:11` (*"THE
> REGISTRY MOVED … TO A PUBLIC …"* → *"bu depo Docker Hub'a push ediyor; WHILE
> private … ONCE public …"*) · `deploy.yml:43` (*"The images live on Docker Hub now"*
> → *"this workflow pushes to Docker Hub … a property of THIS FILE"*) ·
> `deploy.yml:475` (*"With the images on a public … there is no pull credential at
> all"* → *"this workflow no longer writes one; whether one still exists in a cluster
> is not a question this file answers"*) · `deploy.yml:295` (*"NEITHER REPOSITORY
> EXISTS"* → **tarihli snapshot** + *"ilk başarılı push'ta bayatlar; adım buna
> dayanmıyor"*) · `01-rbac.yaml:38` (*"is gone with the move to a public …"* → *"gone
> from THIS REPOSITORY … bir küme hâlâ o Secret'ı tutuyor olabilir"*) ·
> `20-app.yaml:135` (*"The image is downloadable by anyone"* → *"ONCE the repository
> is public …"*).
>
> **KAPSAM BU KEZ SEÇİLMEDİ: `git status --short`'un TAMAMI tarandı** (on dosya,
> orkestratörün `backlog.md`/`state.md`'si dahil — okundu, **değiştirilmedi**).
> **76 aday cümle**, dosya dosya: `deploy.yml` 9 · `Dockerfile` 4 · `README` 10 ·
> `20-app.yaml` 8 · `30-migrate-job.yaml` 5 · `01-rbac.yaml` 4 ·
> `verify-deployment.sh` 4 · `backlog.md` 1 · kart 19 · `state.md` 12. Tasnif:
> **15 koşullu · 9 bu-dosya-hakkında · 32 tarihli defter kaydı · 20 elle okundu**.
> Yirmisinin yirmisi de ya bu dosya/derleme hakkında (Dockerfile'ın `CGO_ENABLED=0`'ı,
> `01-rbac.yaml`'ın *"this identity can no longer …"*'ı), ya geri çekme alıntısı, ya
> genel mekanizma, ya da regex'in yakaladığı YAML/kod bloğu. **Bugünkü küme hakkında
> kalan şimdiki-zaman iddiası: 0.**
>
> **C6 — İDDİA GERÇEĞE İNDİRİLDİ.** `20-app.yaml`'ın *"a later transition needs one
> edit, not four"*'ı ve kartın *"tek bir yer güncellenecek"*i fazla iddiaydı: ölçüldü,
> bugünkü küme durumu `deploy/README.md` içinde **altı ayrı bölgede** tekrarlıyor.
> Doğrusu **tek DOSYA, altı bölge** — ve altısının da **kendi düşme koşulu yazılı**,
> yani hiçbiri sessizce bayatlayamaz. İkisi de düzeltildi.
>
> **🔴 VE BU TURUN KENDİ HATASI, GÜNÜN ÜÇÜNCÜSÜ AYNI SEBEPTEN:** tasnif edicim yine
> **büyük/küçük harfe duyarlıydı** — *"If this says …"* ve *"This workflow no longer
> …"* yalnızca baş harfleri büyük olduğu için *"sınıflandırılamadı"* kovasına düştü.
> Aynı sebep bugün üç kez ısırdı (5. turda `**Dört satırda`, 7. turda `grep -i`'nin
> Türkçe `İ`'yi katlamaması, şimdi bu). `IGNORECASE` ile yeniden koşuldu ve **kalan
> her madde elle okundu** — regex hükmüne değil.
>
> **AÇIK KALAN, DOĞRULANAMADI DİYE YAZILIYOR:** `KUBE_CONFIG` secret'ının **içindeki
> kimlik** okunamadı (ajanın secret okuma yolu yok). Hâlâ cluster-admin bir kubeconfig
> ise `01-rbac.yaml`'daki daraltma **hiçbir şeyi sınırlamaz** — cluster-admin RBAC'e
> bakmaz. README sınır **[kubeconfig'in kimliği doğrulanmadı]**, ve operatör adımı 5
> onu kapatan komutu taşıyor. Ayrıca `pods/exec`'in `resourceNames` daraltması **canlı
> `ns/tappa`'da koşturulmadı** (salt-okuma sınırı) — kum havuzunda birebir kurulumla
> ölçüldü.
>
> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ E — "yedek + PROVALI geri yükleme").
> KRİTER *"Yedek: otomatik, geri yükleme DENENMİŞ"* AĞAÇTA KARŞILANDI, KÜMEDE
> OPERATÖRÜ BEKLİYOR — VE PROVA KARTIN HİÇ ÖNGÖRMEDİĞİ BİR KUSURU BULDU.**
> Değişen dosyalar: **yeni** `deploy/k8s/50-backup.yaml` · **yeni**
> `scripts/pg-backup.sh` · **yeni** `scripts/pg-backup-ship.sh` · **yeni**
> `scripts/pg-restore-verify.sh` · `.github/workflows/deploy.yml` (tek adım, +17
> satır) · `deploy/README.md`. **Go kodu değişmedi** (`git status --short` → **0**
> adet `.go`), `ci.yml` **değişmedi** (`git diff --stat` → boş). **Kümeye hiçbir
> mutasyon uygulanmadı** — kum havuzu bile açılmadı (`kubectl get ns tappa-verify` →
> boş); bütün prova **Docker'da**, ayrı konteynerlerde koştu ve konteynerler silindi.
> Canlı ürün dört ayrı anda ölçüldü: `/healthz` **200**, `/readyz` **200**.
>
> **🔴 KARTIN TUZAK #1'İ (RLS'in sessiz boş yedeği) GERÇEK ÇIKTI AMA MEKANİZMASI
> BRIEF'İN TARİF ETTİĞİNDEN FARKLI — VE FARK ÖNEMLİ.** Brief *"`app.tenant_id` set
> edilmemiş bir bağlantıyla alınan dump sıfır satır üretebilir ve `pg_dump` exit 0
> verir"* diyordu. Ölçüm (bu reponun kendi veritabanı, 231 832 `transactions` satırı):
>
> | komut | sonuç |
> |---|---|
> | `psql` `tappa_owner` (rolsuper=t, rolbypassrls=t) | 231832 satır |
> | `psql` `tappa_app` (f/f), GUC yok | **0 satır** — tuzağın önkoşulu **doğru** |
> | `pg_dump -U tappa_app --data-only -t transactions` | **exit 1**, `ERROR: query would be affected by row-level security policy` — **FAIL-CLOSED** |
> | ...`--enable-row-security` ile | **exit 0**, 966 bayt, **0 satır** — *sessizce boş yedek* |
> | ...GUC bir tenant'a set edilmişken | **exit 0**, 231832'nin **17 262'si** — *sessizce KISMİ yedek* |
>
> Yani `pg_dump` **varsayılan olarak** `row_security = off` kuruyor ve bypass edemeyen
> rol **hata alıyor**; tehlikeli artefakt **tek bir bayrağın** arkasında. Ve üçüncü
> satır brief'te hiç yoktu: kısmi bir yedek, boş olandan **daha** tehlikeli, çünkü
> boyutu makul görünür. `pg-backup.sh` üç katmanda kapatıyor, ikisi **yapısal**:
> bayrak hiç geçilmiyor · rol `rolsuper`/`rolbypassrls` değilse iş `pg_dump`'a **hiç
> ulaşmadan** duruyor (ölçüldü: `tappa_app` ile exit 1 ve adıyla mesaj) · ve bitmiş
> dump'ın satırları canlıyla karşılaştırılıp *"canlıda dolu, dump'ta boş"* olan her
> tablo işi kırmızıya çekiyor.
>
> **🔴 VE PROVANIN BULDUĞU, KARTTA DA BRIEF'TE DE OLMAYAN KUSUR: DÜZ BİR GERİ YÜKLEME
> `tappa_app`'E 31 FAZLA YETKİ VERİYOR — `transactions` ÜZERİNDE **UPDATE VE DELETE
> DAHİL** (§4.3).** Brief'in uyarısı *"geri yüklenmiş bir veritabanı RLS'siz ya da
> GRANT'sız doğabilir"* idi; ölçüm **tam tersini** buldu — GRANT'sız değil,
> **GRANT-FAZLASI**. Sayılar: kaynak **45** tablo yetkisi (sahip hariç), taze bir
> pod'a geri yükleme **76**. Fazlalar arasında `audit_log` DELETE, `admin_users`
> INSERT/UPDATE, `legal_documents` tam CRUD.
> **Mekanizma:** `01-roles.sql` initdb'de `ALTER DEFAULT PRIVILEGES … TO tappa_app`
> koşuyor → geri yüklemedeki her `CREATE TABLE` dördünü de otomatik veriyor →
> `pg_dump` ACL'i **yerleşik** varsayılana göre hesapladığı için fazlalığı geri alan
> `REVOKE`'ları hiç yazmıyor. Dump doğru; hedef boş değil.
> ⚠️ **VE ÜRÜN BOZULMUYOR — kusurun görünmez olmasının sebebi bu.**
> `transactions_no_mutation` tetikleyicisi UPDATE'i **yine** reddediyor. Elle bakan
> biri *"satırı değiştirebiliyor muyum? hayır"* deyip temiz sanar. Fark **yalnız hata
> kodunda**: doğru geri yüklemede `permission denied for table transactions`
> (**42501**), bozukta `append-only table transactions` (**P0001**).
> **Çözüm ölçülerek seçildi, ve seçilen şey bir yama değil bir PROSEDÜR:** geri
> yükleme artık **aynı instance'ta yeni bir veritabanına** yapılıyor — çünkü
> `pg_default_acl` **veritabanı başına** bir katalog (ölçüldü: `tappa`'da 2 satır,
> yeni veritabanında **0**, geri yükleme sonrası dump'ın kendi son iki satırıyla yine
> **2**, yani sonraki migration'lar da doğru davranıyor). Taze pod'a mecbur kalanlar
> için iki satırlık askıya alma adımı yazıldı ve **ölçüldü** (76 → **45**).
>
> **PROVA — ÜÇ KONTROLLÜ KOŞU, BİRİ KASITLI NEGATİF.** Gerçek bir yedekle
> (19 tablo, **2 118 896** satır, 546 MiB düz / **120 MiB** gzip):
>
> | Koşu | Yetki | Doğrulama aracı |
> |---|---|---|
> | taze pod, düzeltme **yok** (negatif kontrol) | **76** (31 fazla) | **exit 1**, 31'ini adıyla listeledi |
> | taze pod, düzeltme **var** | **45** (fazla 0, eksik 0) | **exit 0** |
> | aynı instance'ta yeni DB + `RENAME` takası | **45** | **exit 0** |
>
> Üçünde de: `psql` **exit 0**, stderr **0 satır**, 19/19 tablo, satır sayıları
> manifest ile **birebir**, 18 politika / 18 `FORCE RLS` / 9 tetikleyici / 56 indeks /
> 98 fonksiyon / `goose 20` — hepsi kaynakla aynı. Ve **§4.5 geri yüklemeden sağ
> çıkıyor**: `tappa_app` olarak **TCP üzerinden** (loopback değil — bu reponun
> `01-roles.sql`'inin kendi dersi), GUC'suz **0** satır, GUC'lu **17 262** satır.
> Kaybolan tek şey §4.3'ün **yetki kemeriydi** ve onu yalnız hata kodunu assert eden
> bir kontrol gördü.
> **Süreler:** `pg_dump` 85 sn · doğrulama + gzip ~3 dk · toplam **4 dk 33 sn** ·
> `psql` geri yükleme **37–39 sn** · doğrulama aracı **23 sn** · iki
> `ALTER DATABASE RENAME` **399 ms** · boş bir şemanın yedeği **< 1 sn**.
>
> **DEJENERE GİRDİLER — KENDİ LİSTEM, KOŞTURULDU.** `pg-backup.sh` dokuz yol
> (**boş ama sağlıklı veritabanı → exit 0** · `tappa_app` ile → rol kapısı · **tablosu
> olmayan** veritabanı → adıyla ret · ulaşılamayan host · yanlış parola · `PGPASSWORD`
> yok · staging yok · staging salt-okunur · **staging dolu (8 MiB tmpfs)** →
> `No space left on device` ve hiçbir şey sevk edilmiyor). `pg-backup-ship.sh` on iki
> yol (config yok/boş/`RCLONE_CONFIG` yok · `BACKUP_REMOTE` yok/çıplak/sondaki eğik
> çizgi/remote değil/tanınmayan · staging boş · manifest'siz dump · saklama süresi
> sayı değil / 7'nin altında). **Hepsi adıyla mesaj verdi.**
> 🔴 **VE BU BATARYA KENDİ KODUMDA GERÇEK BİR HATA BULDU:** `: > "$probe" || fail …`
> salt-okunur bir mount'ta **kendi mesajını hiç basmıyordu** — `:` bir POSIX **özel
> builtin**'i ve yönlendirme hatası kabuğu **anında** sonlandırıyor, yani `||` hiç
> koşmuyor. `touch`'a çevrildi ve yeniden ölçüldü. Aynı sınıfın bir başka örneği
> baştan kaçınılarak yazıldı: sayımların **hiçbiri** `grep -c` kullanmıyor, çünkü
> `grep` sıfır eşleşmede exit 1 verir ve o, **sağlıklı boş veritabanı** yoludur.
>
> **ŞİFRELEME VE HEDEF — ÖLÇÜLDÜ, GERÇEK BİR `crypt` REMOTE'A KARŞI.** rclone'un
> `crypt` remote'u yerel bir backing store üzerinde koşturuldu: hedef diskteki dosya
> **adları** okunamaz (`uopkhhceg3gu…/e4cqqtph…`), **içerik** `RCLONE\0\0` sihirli
> baytıyla başlıyor, aynı hedef `rclone lsl` ile açık adlarla listeleniyor. Saklama
> süresi de izole edilerek ölçüldü: 2026-01-01 damgalı bir yedek **silindi**, o
> geceninki **kaldı**, boş dizin `rmdirs` ile toplandı.
> ⚠️ **`rclone check` bir crypt remote'ta `ERROR : No common hash found` basıyor ve
> bu bir arıza DEĞİL** (aynı çıktıda `0 differences found`, exit 0;
> `--ignore-checksum` bastırmıyor) — gecelik logda `ERROR` arayan operatörü
> yanıltmasın diye script'te ve README'de yazılı.
>
> **HEDEF SEÇİMİ: ÖLÇÜLDÜ, VE KÜMEDE HİÇBİR SEÇENEK YOK.** `kubectl get nodes` → tek
> node · `get sc` → yalnız `local-path` · `get csidrivers` → **boş** · `get pv` →
> 20/20 `local-path` · `get cronjob -A` → hiçbir namespace'te yedek işi **yok** (emsal
> de yok). Yani off-node hedef **kaçınılmaz olarak operatörün kimliğini** istiyor.
> **Elenenler, gerekçesiyle:** kümedeki bir PVC (aynı node'un diski — sınır 1'in
> tarif ettiği arızada hiçbir şey kurtarmaz) · GitHub Actions'ın dump'ı çekmesi
> (mesai kayıtları AB dışına ve DPA'sız bir işleyene — Q23) · ikinci bir Postgres
> replikası (tek node'da aynı disk; ayrıca yanlış bir `DELETE` replikaya **anında**
> kopyalanır — replika yedek değildir). **Seçilen:** AB bölgesinde S3 uyumlu nesne
> depolama ya da SFTP, ikisi de **tek bir `rclone.conf`** ile.
>
> **NEDEN CronJob OPERATÖRÜN, `deploy.yml`'in DEĞİL — ve bu FAZ D'nin dersinin
> uygulaması.** `01-rbac.yaml` deploy kimliğine `batch` grubunda yalnız `jobs`
> veriyor. `cronjobs` eklemek, **önce** cluster-admin ile 01-rbac.yaml'ın yeniden
> uygulanmasını gerektirirdi ve o ana kadar **her deploy kırmızı** olurdu — yani
> kartın imza kusurunun bir başka yüzü. Script'ler ise commit yolunda kalıyor:
> `deploy.yml` ConfigMap'i `--from-file` ile her koşuda yeniden kuruyor
> (`tappa-db-init` kalıbının aynısı), yani **tek tanım**, `scripts/` altında, ve
> `redline-check.sh`'in `SRC` listesinin **içinde** — `deploy/`'un dışarıda kalması
> (C4) bu sefer bir boşluk üretmedi.
> ⚠️ **Ve tam da o tarayıcı benim dosyamda bir bulgu üretti:** `pg-restore-verify.sh`
> ilk hâlinde çıplak `SET app.tenant_id` yazıyordu (R5 · WARN). Tek atışlık bir
> `psql` oturumunda havuz kirlenmesi **olamaz**, ama kural izlendi ve
> `BEGIN; SET LOCAL …; ROLLBACK;` yapıldı: bir sondanın uygulamayı modellemesi
> gerekiyorsa tenant bağlamını da uygulamanın yazdığı gibi yazmalı, ve tartışılması
> gereken bir tarayıcı görmezden gelinen bir tarayıcıdır.
>
> **SINIR LİSTESİ: 1 YENİDEN YAZILDI, 3 YENİ, 2 GÜNCELLENDİ — ve liste `1..21`
> ardışık, **0 sarkan anahtar atfı** (ölçüldü: tanımlı 15 anahtar, atıf yapılan 15,
> fark **boş**).** **[yedek YOK]** anahtarı **emekli edildi** çünkü artık yalan
> olurdu; yerine **[yedek kümede HENÜZ YOK]**, ve madde ağaç/küme ayrımını **adıyla**
> yazıyor (`kubectl -n tappa get cronjob` → **`No resources found`**, ölçüldü
> 2026-08-16). Yeni: **[yedeğin hedefi ve şifresi KÜME DIŞINDA yaşar…]** (19),
> **[yedek saklama süresi 30 gün…]** (20), **[geri yükleme AYRICALIK ARTIĞI üretir…]**
> (21). Güncellenen: **13** (yeni CronJob kuralın üçüncü tüketicisi ve `wait-for-postgres`
> taşıyor — kural **kapanmadı**, uyuldu) ve **14** (`rclone/rclone:1.71` hareketli
> etiketin **üçüncü** yazımı; digest pinleme artık dört yazım demek).
>
> **KARTIN HÂLÂ AÇIK KRİTERLERİ, açıkça:** *"managed Postgres"* (kullanıcı kararı) ·
> **KEK döndürme aracı** (sınır 9, **bu turda yapılmadı** — FAZ F) · **DPA/Q23 +
> saklama süresi** (hukuki, backlog B3). Ve *"geri yükleme denenmiş"* kriteri için
> dürüst cümle şu: **prosedür provalı, araç var, kümede yedek yok** — üçüncüsü
> operatör adımı 9'da.
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 2. TUR — GENEL ÜÇÜNCÜ GÖZ ONAY, `tappa-security-auditor`
> RED. BULDUĞUM KUSUR SANDIĞIMDAN AĞIRMIŞ: §4.3 DEĞİL, §4.4 + §4.7.**
>
> **B1 — 31 fazla yetkinin ikisi `tags` üzerinde, ve `tags`'te İKİNCİ KEMER YOK.**
> 1. turda *"§4.3'ün iki kemerinden biri düştü, tetikleyici hâlâ reddediyor, ürün
> bozulmuyor"* yazdım ve bunu **üç yerde** tekrarladım. Denetçi ölçtü:
> `tags_counter_monotonic` yalnız `BEFORE UPDATE … WHEN (new.last_ctr < old.last_ctr)`,
> yani **INSERT'te hiç ateşlemiyor**. Kendi yeniden üretimim, geri yüklenmiş gerçek
> şemada, `tappa_app` ile TCP üzerinden, tap kaydı olmayan bir plakette:
> ```
> A  ctr before                        5100
> DELETE FROM tags WHERE uid=...    -> DELETE 1     (yetki YALNIZ artıktan geliyor)
> INSERT ... last_ctr 0             -> INSERT 0 1
> C  ctr AFTER delete+reinsert         0
> D  a REPLAYED ctr=7 now advances to  7
> E  aes_key_ref is now                forged-key-ref
> ```
> **§4.4'ün replay koruması çalınmış bir `tappa_app` kimliğiyle tamamen sıfırlanıyor**,
> ve tablo düzeyi UPDATE sütun kısıtını ezdiği için `aes_key_ref` **yazılabilir**
> (`has_column_privilege`: kaynakta false, geri yüklemede **true**). `01-roles.sql`
> bu tehdit modelini adıyla yazıp *"§4.3 ayakta kalır"* diyordu — `tags` için
> kalmıyordu.
>
> **KALICI ÇARE ÖLÇÜLDÜ VE UYGULANDI — VE ÖLÇÜM DENETÇİNİN İDDİASINI HEM DOĞRULADI
> HEM DARALTTI.** Denetçi *"iki satırlık askıya alma 76 → 45 yapıyor"* dedi; o ölçüm
> **askıya alma adımına** aitti, önerdiği **daraltmaya** değil. İkisini ayrı ölçtüm:
>
> | kurulum | tablo yetkisi | `tags` UPDATE/DELETE | `aes_key_ref` UPDATE |
> |---|---|---|---|
> | kaynak (canlı şema) | **45** | false / false | false |
> | geniş varsayılan + geri yükleme (bugünkü ağaç) | **76** | **true / true** | **true** |
> | **dar varsayılan** + geri yükleme | **50** | false / false | false |
> | dar varsayılan + askıya alma + geri yükleme | **45** | false / false | false |
>
> Yani **daraltma ağır yarıyı yapısal olarak kapatıyor** (replay zinciri artık ilk
> adımda `permission denied for table tags` ile duruyor) ama **sıfıra indirmiyor**:
> 5 fazla kalıyor, aralarında `legal_documents` üzerinde tablo düzeyi SELECT — o da
> kendi sütun listesini ezer. **İkisi birlikte 45/0/0.**
>
> **DARALTMA TAZE KURULUMU BOZMUYOR — GERÇEK goose İLE ÖLÇÜLDÜ, İDDİA EDİLMEDİ.**
> `Dockerfile --target migrate` ile gerçek migration imajı derlendi ve iki taze
> kurulum (geniş / dar varsayılan) üzerinde 20 migration koştu; **ikisi de başarılı**,
> ve tablo yetkileri arasındaki **TEK fark** `goose_db_version` üzerindeki
> UPDATE+DELETE (goose'un kendi defteri, uygulama ona dokunmaz). Uygulama
> tablolarının hepsi **birebir**: 45 = 45, ve geniş kurulum canlı seed'lenmiş
> veritabanıyla da birebir (harness'ın kendisi böyle doğrulandı). `go test -race
> -count=1 ./...` dar veritabanına karşı (kullanıcının `tappa-db`'sine
> **dokunmadan**, ayrı bir konteynere `DATABASE_URL`/`DATABASE_MIGRATE_URL` ile):
> **exit 0, 22 paket ok, 0 FAIL, 0 atlanan DB testi**, ve testlerin gerçekten oraya
> yazdığı 700 tenant satırıyla kanıtlandı. Sebep de zaten yapıda: her migration
> istemediğini **açıkça REVOKE ediyor** (`REVOKE DELETE ON tags`,
> `REVOKE UPDATE, DELETE ON transactions`, … 23 ifade), yani hiçbiri varsayılana
> dayanmıyordu. ⚠️ `01-roles.sql` bir **init script'idir**, migration değil: yalnız
> boş `PGDATA` üzerinde koşar, yani **canlı veritabanını değiştirmez** — kazancı taze
> kurulumlar ve taze pod'a geri yüklemelerdir. Üç metin (araç başlığı, sınır 21, bu
> defter) *"yalnız §4.3"*ten **§4.4 + §4.7**'ye çekildi.
>
> **B2 — TEK KESİN *"EVET"* SENARYOSUNUN NUMARALI PROSEDÜRÜ YOKTU.** Karar tablosunun
> tek kesin EVET'i *"node'un diski gitti"*, ama sekiz adımın tamamı hasarlı
> instance'ın var olmasını varsayıyordu; taze pod yolu adım 4'ün içinde bir ⚠️ notuydu.
> Artık **A YOLU / B YOLU** ayrımı iki komutla başlıyor ve **B YOLU ayrıca numaralı**
> (B1–B6): **askıya alma B2, yani birinci sınıf bir adım**, doğrulama aracı B4'te
> **zorunlu** (*"çıkmazsa exit 0, uygulamayı açma"* — o yolda karşılaştırılacak bozuk
> bir veritabanı **yok**, yani tek kanıt odur), ve B5 kayıp aralığının **ilan
> edilmesini** istiyor çünkü orada ölçülecek kaynak yok. G1 ve G2 de bununla kapandı.
>
> **YEDİ UCUZ — hepsi ölçümle.**
> **S1** Doğrulayıcı **91 `ON TABLE` GRANT satırının 73'ünü** atlıyordu (sütun düzeyi),
> ve atlanan kümede `aes_key_ref`'i UPDATE dışında tutan **tek mekanizma** vardı.
> Artık iki düzey de karşılaştırılıyor: dump'ın sütun grant'ları **artı** tablo
> grant'larının sütunlara yayılmışı, `information_schema.column_privileges` ile
> birebir. 🔴 **Ve ilk hâli 39 YANLIŞ POZİTİF üretti** (*"missing"*), çünkü SQL'de
> **sütun düzeyi DELETE yoktur**; yayılım artık yalnız SELECT/INSERT/UPDATE/REFERENCES
> için yapılıyor (ölçüldü: `column_privileges` yalnız bu dördünü raporluyor).
> Sonuç: doğru geri yüklemede **45 tablo + 454 sütun**, exit 0; artıklı olanda
> **5 tablo + 11 sütun** fazlası adıyla, exit 1.
> **S2** Kayıp aralığı komutlarında `-v ON_ERROR_STOP=1` **yoktu**; ölçüldü: bozuk bir
> `COPY` bayraksız **exit 0**, bayrakla **exit 3** — yani boş bir CSV *"kayıp yok"*
> diye okunup adım 8 atlanabilirdi. İki `psql`'e de eklendi.
> **S3** 🔴 CSV `SELECT *` idi ve `gps_lat`/`gps_lng numeric(9,6)` **tam koordinat**
> dışa aktarıyordu (§4.2/§4.7). Artık 13 sütun **adıyla**; dışarıda bırakılanlar ve
> neden yazılı (GPS · `source_ip` · `tag_uid`/`ctr`/`sun_valid` · `trust`/`*_match`/
> `policy_*`). Yeniden giriş `channel='manual'` olduğu için hiçbirine ihtiyaç yok.
> Ölçüldü: 745 satırlık gerçek çıktıda **koordinat şeklinde tek alan yok** (17 *"gps"*
> eşleşmesinin hepsi §5'in `note` metni).
> **S4** İki Secret'a bölmenin gerekçesi **iddia ettiği özelliği sağlamıyordu** —
> ikisi de aynı namespace/etcd/node'da. Gerekçe *"ayrı okuyucu kümesi"*ne indirildi,
> ve emanet artık **kanıtlanıyor**: adım 9(b) küme içi ve emanetteki `rclone.conf`'un
> sha256 **öneklerini** karşılaştırıyor (değer basılmadan).
> **S5** `redline-check.sh` R3 şema nitelemesini **tanımıyordu** (`UPDATE
> public.transactions` → eşleşmiyor), ve nitelemeli ilk mutasyonu bu turun kendi aracı
> getirdi — yani kural **yanlış sebeple** yeşildi. Desen düzeltildi (istege bağlı şema
> öneki + tırnaklı tanımlayıcı); **mutasyon testiyle** doğrulandı: ekilmiş bir
> `UPDATE public.transactions` → **exit 1**, kaldırılınca **exit 0**; kontroller
> (`transaction_reviews`, `transactions_archive`, `SELECT`, `INSERT`) eşleşmiyor.
> Doğrulama aracına **dosya adıyla** dar bir muafiyet verildi ve bedeli sayıldı.
> ⚠️ **Kapatılmayan, sayılan:** R7'nin sır-log deseni **Go biçimli**, yani bu üç kabuk
> script'inde bir `echo "$PGPASSWORD"` **hiçbir kuralca yakalanmaz** (bugün temiz).
> **S6** `BACKUP_REMOTE` (kova + yol) iş loguna basılıyordu; `pods/log` yetkisi olan
> herkes okur. Artık yalnız **remote adı**, yol *redacted*.
> **S7** *"nothing left behind"* **bilinenden fazlasını iddia ediyordu**:
> `failedJobsHistoryLimit: 3` ve TTL yok, yani düşen Job'ın pod'u **tutuluyor**; pod
> nesnesi dururken kubelet'in `emptyDir`'i söküp sökmediği **ölçülmedi** ve öyle
> yazıldı, temizlik komutuyla birlikte.
> **G3** `kept="$(rclone lsf … 2>/dev/null | awk …)"` — listeleme düşerse **başarılı
> bir yüklemeden sonra** `done: 0 backup(s) retained` + exit 0 basıyordu; en
> ürkütücü cümle, en zararsız sebepten. Artık exit kodu ayrılıyor ve *"UNKNOWN (not
> zero)"* deniyor.
> **G4** awk sayacının `^COPY public\.` çapası **sayılmış limit** olarak yazıldı
> (bugün ulaşılamaz: hiçbir tablonun ilk sütunu serbest metin değil; olsaydı arıza
> yine **gürültülü** olurdu, çünkü 4b o tabloyu *"veri bölümü yok"* diye bildirir).
> **G5** Boş config korumasının **mekanizması yanlıştı** (*"rclone yerel yol sayıp pod
> içine yazar"*); ölçüldü (v1.71.2): tanımsız remote → `CRITICAL … didn't find section
> in config file`, exit ≠ 0, **dizin yaratılmıyor**. Koruma doğru, gerekçe düzeltildi.
> **G6** `grep -q 'a\|b'` BRE alternasyonu iki düz `grep -qF`'ye çevrildi.
>
> **DEĞİŞMEYENLER:** kümeye hâlâ **hiçbir mutasyon** uygulanmadı
> (`kubectl -n tappa get cronjob` → `No resources found`), kullanıcının `tappa-db`'si
> **yalnız okundu** (231 832 satır, başta ve sonda aynı), `ci.yml` **değişmedi**,
> `git status --short` → **0 adet `.go`**, canlı ürün `/healthz` `/readyz` **200**.
> **Yeni dosya sayısı değişmedi**; değişen: `scripts/db-init/01-roles.sql` (tek satır
> + gerekçe) ve `scripts/redline-check.sh` (R3 deseni).
>
> ---
>
> **✅ [FAZ E · 2026-08-16] 4. TUR — `tappa-security-auditor` ONAY. ÜÇ UCUZ, VE İKİSİ
> "SAĞLANMAYAN BİR GARANTİYİ İLAN ETMEK" SINIFININ YENİ ÖRNEKLERİYDİ.**
>
> **S1 — DARALTMA, İHLALİN EN PAHALI OLACAĞI İKİ VERİTABANINDA YÜRÜRLÜKTE DEĞİL.**
> `01-roles.sql`'e yazdığım *"unutulursa gürültülü `permission denied` alınır
> (fail-closed)"* cümlesi **koşulsuzdu** ve iki yerde yanlıştı. (a) Dosya bir **init
> script'i**: çalışan veritabanlarının `pg_default_acl`'i hâlâ `tappa_app=arwd`.
> (b) 🔴 **Ve dump'ın kendi son satırı varsayılanı yeniden GENİŞLETİYOR** —
> `GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO tappa_app`, yani **her geri
> yüklemenin sonunda** hedef yine geniş. Dört adımda ölçüldü: dar init → **`ar`** ·
> geri yükleme sonrası → **`arwd`** · o anda yaratılan yeni tablo `tappa_app`'e
> **`arwd`** veriyor · var olan tablolar **etkilenmiyor** (45 yetki, `tags`
> UPDATE/DELETE false). **Somut sonuç: doğrulama ortamı üretimden KATI, ve ayrım
> kusuru GİZLER yönde** — `REVOKE` satırını unutan bir migration yerelde hiçbir fark
> üretmez, üretimde sessizce `arwd` verir; `pg-restore-verify.sh` de göremez, çünkü
> referansı dump'ın kendisi ve dump geniş varsayılanı **ilan ediyor**.
> ✅ **Orkestratörün *"değerlendir"* dediği çare ÖLÇÜLDÜ VE UYGULANDI:** geri
> yüklemenin **son adımı** tek ifadeyle geri daraltıyor
> (`REVOKE UPDATE, DELETE ON TABLES` — `SELECT, INSERT` bilerek kalıyor). Ölçüm:
> varsayılan `ar`'a dönüyor, yeni bir tablo `ar` alıyor, ve **var olan tablolar
> değişmiyor** (45 yetki · `tags` false/false · `aes_key_ref` false · sekans
> varsayılanı `rU` sabit). A YOLU 7. adıma ve B YOLU B3'e girdi, heredoc biçimi
> **birebir koşularak** sınandı (exit 0). ❌ **Çalışan veritabanında dayatılmadı** —
> canlı bir DB'yi migration dışında değiştirmek bir karardır; sınır **22** olarak
> sayılıyor, komutuyla birlikte. Üç metin (`01-roles.sql`, README'nin iki *"doğru
> davranır"* cümlesi) kapsamı artık **açıkça** yazıyor.
>
> **S2 — MASKE YALNIZ BAŞARI YOLUNDAYDI.** `BACKUP_REMOTE`'un kova+yolu iki **arıza**
> dalında maskesiz basılıyordu — üstelik biri script'in kendi yorumunun *"en olası uzun
> vadeli arıza"* dediği dal. Tek bir `REMOTE_SAFE` yazımı tanımlandı ve **her** mesajda
> kullanılıyor. ⚠️ **Ve kalan kısmı sayılıyor, yarım kapatılmıyor:** `rclone`'un
> **kendi** tanılama satırları remote'u adıyla basıyor (ölçüldü:
> `NOTICE: Encrypted drive 'enc:db/<damga>' …`). Maskeleyen bir filtreye **borulanmadı**,
> çünkü `set -eu` borunun solundaki hatayı göremez ve bir §4.7 sıyrığını fail-open bir
> çıkış koduyla takas etmek bu reponun iki kez bedelini ödediği takastır.
>
> **S3 — HEDEFİN GERÇEKTEN `crypt` OLDUĞUNU DOĞRULAYAN HİÇBİR ŞEY YOKTU** (kelime
> yalnız bir yorumda geçiyordu). Düz bir S3 kovası verilseydi her tap'in **tam
> GPS'i**, her plaketin **sarmalanmış AES anahtarı** ve her **oturum token hash'i**
> şifresiz node dışına çıkar, iş **yeşil** kalırdı. Artık remote'un **tipi
> sorgulanıyor**, `alias` zinciri izleniyor (5 sıçrama sınırı), ve `crypt` değilse
> **hiçbir şey gönderilmeden** duruluyor. **Dokuz dejenere yol koşuldu:** `crypt` ✓ ·
> `local` ✗ · `s3` ✗ · `alias→crypt` ✓ · `alias→local` ✗ · tanımsız ✗ · hedefsiz alias
> ✗ · alias döngüsü ✗ · `crypt`→`crypt` ✓; artı iki kontrol: önek karışması
> (`enc` vs `enc2`) **yok**, ve dört koşuda parola/markör çıktıda **0 kez**.
> 🔴 **§4.7 ölçümü aracın seçimini belirledi:** markörlü bir parolayla
> `rclone config show` **`password = *** ENCRYPTED ***`** basıyor (obscure değer 0,
> düz metin markör 0) ama `config dump` obscure değeri **2 kez** döküyor — bu yüzden
> `config show`, ve section'dan **yalnız `type` satırı** okunuyor. Sınır **19** artık
> dört ölçülemeyen özelliğin yanına **ölçülen beşinciyi** yazıyor.
>
> **🔴 VE BU TURUN İKİ KENDİ HATASI, DEFTERE.**
> **(1) §4.7 ölçümümün İLKİ GEÇERSİZDİ.** *"config show parolayı sızdırıyor mu"* diye
> baktım ve `grep -c "$OBS"` **19** dedi — oysa test config'imde `password` satırı
> **hiç yoktu**, yani `$OBS` boştu ve `grep -c ""` her satırı saydı. Markörlü bir
> parolayla yeniden kurdum. **Bu, bu reponun `01-roles.sql`'inde yazılı olan dersin
> aynısı** (*"bir doğrulama, tükettiği şeyin GÖRDÜĞÜ biçimi görmek zorundadır"*) — ve
> sayının büyüklüğü onu ele verdi, cevabın kendisi değil.
> **(2) Dejenere bataryam gerçek bir kusur buldu: `rclone check` YANLIŞ KAPSAMLIYDI.**
> İki damga taşıyan bir staging dizininde, doğru yüklenmiş bir yedekten sonra
> *"2 differences found"* deyip iş **kırmızı** oluyordu — çünkü karşılaştırma **tüm
> dizini** tek bir damganın hedefiyle eşliyordu. CronJob'da staging taze bir
> `emptyDir` olduğu için orada **oluşamaz**; elle koşan biri için oluşur. `--include
> "tappa-$stamp.*"` ile kapatıldı ve **iki biçimde de** ölçüldü: iki damgalı dizin
> **yeşil**, tek damgalı dizin **yeşil**. ⚠️ Kusuru bulan şey, testimin kendi
> dağınıklığıydı — ve bu tam olarak *"dejenere girdileri kendin say ve koştur"*un ne
> için olduğudur.
>
> **DEĞİŞMEYENLER:** kümeye **hiçbir mutasyon**, kullanıcının `tappa-db`'si yalnız
> okundu, `ci.yml` **değişmedi**, **0 adet `.go`**, canlı ürün **200/200**. Sınır
> listesi **1..22 ardışık**, **0 sarkan anahtar** (16 tanımlı / 16 atıf).
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BLOKLAYAN: `crypt`
> KONTROLÜM ŞİFRELEMEYİ DEĞİL, `type` SATIRINI ÖLÇÜYORDU — VE BU, OTURUMUN İMZA
> SINIFININ BENİM ELİMDEN ÇIKMIŞ HÂLİ.**
> Denetçi sevk edilen imajla ölçtü: **gerçekten `type = crypt`** olan bir remote'a tek
> bir seçenek eklemek yetiyordu — `no_data_encryption = true` ile script *"destination
> is an encrypted (crypt) remote"* deyip yüklüyor, doğruluyor ve **exit 0** veriyordu,
> oysa hedefteki dump **düz gzip** (`037 213 \b`) ve manifest **`cat` ile okunabilir**.
> 🔴 **Kendi dokuz dejenere yolumun dokuzu da yalnız `type` DEĞERİNİ değiştiriyordu;
> crypt'in KENDİ seçeneklerini bir kez bile değiştirmemiştim** — yani dedektör, bir
> önceki kusurun şekline göre yazılmıştı. Defterin kendi cümlesi, üçüncü kez.
>
> **SEÇİLEN ÇARE (2): BEYAN DEĞİL ÖZELLİK ÖLÇÜLÜYOR.** Yüklemeden **önce** crypt
> remote'una bir kanarya yazılıyor, **altındaki** remote'tan geri okunuyor ve iki şey
> assert ediliyor: saklanan baytlar rclone'un `RCLONE\0\0` sihirli baytıyla başlamalı,
> ve saklanan **ad** kanaryanın düz metin markörünü içermemeli. Neden yüklemeden önce:
> sonra koşan bir kontrol, hedefin şifresiz olduğunu ancak **çalışan kayıtlarını oraya
> yazdıktan sonra** kanıtlardı.
>
> **DEJENERE YOLLAR, BU KEZ CRYPT'İN KENDİ SEÇENEKLERİ ÜZERİNDE — on beşi de
> fail-closed, çıkış kodlarıyla:**
>
> | yol | rc | yol | rc |
> |---|---|---|---|
> | varsayılan crypt | **0** | `no_data_encryption = true` | **1** |
> | `filename_encryption = standard` | **0** | `filename_encryption = off` | **1** |
> | `filename_encryption = obfuscate` | **0** | ikisi birden | **1** |
> | alias → iyi crypt | **0** | `no_data_encryption = TRUE` (büyük harf) | **1** |
> | | | `no_data_encryption =␣true␣` (boşluklu) | **1** |
> | | | crypt, `remote =` yok | **1** |
> | | | alias → `nodata` crypt | **1** |
> | | | düz `local` · tanımsız remote | **1** |
> | | | bağlantı dizgisi · on-the-fly `:local:` | **1** |
>
> 🔴 **Büyük harf ve boşluklu varyantlar bu yaklaşımın neden seçildiğinin kanıtı:**
> özellik, seçeneğin nasıl **yazıldığını** umursamıyor. Bir `config show` ayrıştırıcısı
> (seçenek 1) o ikisinde kolayca yanılırdı.
> **Ve "kanıtlanamadı" ile "şifresiz" AYRI iki mesaj:** kanarya yazılamazsa /
> `cryptdecode` boş dönerse / alttaki remote okunamazsa iş *"COULD NOT BE PROVEN …
> this is NOT a claim that the destination is unencrypted"* diyor ve **yine hiçbir şey
> yüklemiyor**. ⚠️ Bu üçünden ikisi (kanarya yazımı, `cryptdecode`) fiilen tetiklendi;
> *"yazıldı ama alttan okunamadı"* dalı yerel bir backing store ile **üretilemedi** →
> **doğrulanamadı**, ve öyle yazıldı.
>
> **B1 — YENİ KONTROL, EN OLASI OPERATÖR HATASINDA TAMAMEN SESSİZ ÖLÜYORDU.**
> `rclone config show "$1" 2>/dev/null` + `set -eu` altında `||`'siz atama: elle
> yapıştırılmış bir Secret'ın **tek bozuk satırı** rclone'u `CRITICAL: Failed to load
> config file … could not parse line` ile düşürüyor, `2>/dev/null` sebebi yutuyor ve
> script **sıfır satır çıktıyla** ölüyordu (`rclone` ikilisi yoksa exit 127, aynı
> sessizlik). ⚠️ **Benim test ettiğim yol — tanımsız remote — `config show`'un exit 0
> verdiği yoldu**, yani sessiz olmayan tek yol. Artık stderr saklanıyor, çıkış kodu
> kontrol ediliyor ve rclone'un kendi cümlesi mesaja giriyor; bozuk config ile
> **ölçüldü**.
>
> **B2 — DOĞRULAYICI, BAĞLANTI HATASINI *"FORCE RLS yürürlükte değil"* DİYE TEŞHİS
> EDİYORDU.** Guard **iki dizeyi** sayıyordu; `CONNECT` alınmış bir DB'de gerçek hata
> `FATAL: permission denied for database "tappa"` ve o listede yok. Yapısal biçime
> çevrildi: sonda ilk ifadesinde bir **markör satırı** basıyor; markör yoksa oturum
> **hiç koşmamıştır**, sebebi ne olursa olsun. Ölçüldü — `CONNECT` alınmış DB:
> *"the tappa_app probe produced no answer … neither a pass nor an RLS finding. Fix the
> connection first. psql said: … permission denied for database"*; `CONNECT` geri
> verilince aynı komut **ok / ok / 42501**.
>
> **B3 — BÜTÜNLÜK KOMUTU OPERATÖRÜN KENDİ MAKİNESİNDE ÇALIŞMIYORDU.**
> `sha256sum -c <<<"…"` bu makinede (`sha256sum (Darwin) 1.0`) **`usage:` + exit 1**,
> Debian+GNU'da `OK` — yani **sağlıklı bir yedek**, kesinti anında *"yedek bozuk"* gibi
> okunan bir hata üretiyordu. Bu, README'nin 118–121. satırlarında **bedeli zaten
> ödenmiş** kalıp (*yerel araç ≠ hedef araç*), ve provam Linux konteynerlerinde
> koştuğu için o satır operatörün platformunda **hiç koşulmamıştı**. İki değişkeni
> karşılaştıran biçime çevrildi ve **iki lezzette de** ölçüldü. ⚠️ Ayrıca `$STAMP`
> artık elle veriliyor: `./restore/*.manifest` glob'u **iki damga** varsa iki hash
> basıp sessizce yanlış eşleştiriyordu (ölçüldü).
>
> **B4 — SAYILMIŞ SINIR DAR SAYILMIŞTI.** *"rclone'un kendi çıktısı yolu basıyor —
> **arızalı koşuda**"* yazmıştım; ölçüm: **exit 0 olan sağlıklı koşuda da** iki satır
> (`0 differences found` / `2 matching files`). Yani **her gece**. Yön doğruydu,
> kapsam yanlış; düzeltildi.
>
> **B5 — kapsam dışı, sayıldı:** `pg-backup.sh`'te `||`'siz komut-ikamesi atamaları
> **bilerek** duruyor ve gerekçesi dosyada: hepsi rol kapısının **kanıtlanmış**
> bağlantısından sonra koşuyor, ilk temasın kırılabildiği dört yol ise adlandırılmış
> mesaj taşıyor. Aynı şekil ship script'inde **kabul edilmedi**, çünkü orada config
> dosyasının kendisini koruyordu.
>
> **🔴 BU TURUN KENDİ HATALARI, DEFTERE.** (1) Özet harness'ım `cut -c` ile emoji'li
> satırları kırpıp **boş** gösterdi ve üç FAIL'i bir an *"mesajsız"* sandım — çıktının
> kendisi doğruydu, **ölçüm aracım** yanlıştı. (2) İlk doğrulama tablomda `set -- $c`
> kullandım; **zsh tırnaksız parametreyi kelimelere bölmez**, yani *beklenen* sütunu
> boş kaldı ve dört satır sahte *"MISMATCH"* gösterdi. İkisi de yalnızca raporlama
> katmanındaydı ve ikisi de temiz komutlarla yeniden koşuldu — ama ikisi de aynı
> dersin küçük hâli: **ölçüm aracının kendisi bir iddiadır.**
>
> **DEĞİŞMEYENLER:** kümeye hiçbir mutasyon, `tappa-db` yalnız okundu, `ci.yml`
> değişmedi, **0 adet `.go`**, canlı **200/200**.
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 7. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BLOKLAYAN: KANARYANIN
> GERİ-OKUMA YOLU YANLIŞ ADRESE BAKIYORDU — VE BU, BİR TUR ÖNCE *"ÜRETİLEMEDİ"*
> DEDİĞİM DALIN TA KENDİSİYDİ.**
> `rclone cat "$crypt_under/$canary_stored"`. Crypt'in `remote =` değeri iki nokta
> üst üsteden sonra **yol taşımıyorsa** (`remote = back:`) bu ifade **`back:/<ad>`**
> üretiyor — **mutlak** bir yol; oysa kanarya remote'un **köküne** yazılmıştı.
> Sevk edilen imajla, kusursuz şifreleyen bir depoda ölçüldü:
> ```
> rclone cat back:<ad>    ->  R C L O N E \0 \0    rc=0     (gerçek)
> rclone cat back:/<ad>   ->  ERROR: error listing: directory not found, rc=3
> ```
> **Somut sonucu:** hedef **doğru şifrelerken** iş **her gece kırmızı**, **hiç yedek
> çıkmıyor**, ve mesaj operatörü **kimlik/ağ** aramaya gönderiyor — sebep
> `rclone.conf`'un tek satırıyken. 🔴 **Ve `remote = <ad>:` marjinal değil:** rclone'un
> **kendi sihirbazının belgelediği** biçim, ve README'nin önerdiği iki hedeften biri
> olan **SFTP** için **doğal** yazım.
> 🔴 **İKİ KENDİ HATAM BURADA BİRLEŞİYOR.** (1) Geçen tur *"'yazıldı ama alttan
> okunamadı' dalı yerel bir backing store ile üretilemedi"* yazmıştım — **üretildi**,
> tek config satırıyla. *Bir şeyin üretilemez olduğu iddiası da bir ölçümdür, ve o
> ölçüm yanlıştı.* (2) Aynı bloğun yorumu *"no alias arithmetic, **one code path**"*
> diyordu; `"$crypt_under/…"` **bir yol aritmetiğidir** ve bir yazımda yanlıştı — yani
> yorum, kodun yapmadığı bir şeyi ilan ediyordu.
> **Düzeltme bir `rjoin` yardımcısı:** sondaki `/` kırpılır, taban `:` ile bitiyorsa
> ad **doğrudan** eklenir, aksi hâlde `/` ile. **`remote =` yazımları üzerinde yedi
> dejenere yol, hepsi rc=0:** `back:` · `back:sub` · `back:sub/` · `back:sub/deeper` ·
> `/mutlak` · `/mutlak/` · alias arkasında; ve `back:` + `no_data_encryption=true`
> hâlâ **rc=1**. Kalıcı bir backing store'la tekrarlandı: hedefte gerçek nesneler
> **`RCLONE\0\0`** ile başlıyor, reddedilen koşuda hedef **0 nesne** (yani
> *"yüklemeden önce"* kararı yine kanıtlandı), ve **hiçbir yerde kanarya kalmıyor**.
> ⚠️ İlk bataryamda üç vaka **0 nesne** yazdı çünkü `local` backend'inde `root =`
> diye bir seçenek **yok** — sondam kendi kurgusuyla yanılmıştı; alias'lı kurulumla
> yeniden koşuldu.
>
> **SEKİZ MADDE.**
> **B1** Doğrulayıcının *"yapısal"* guard'ı yapısal değildi: markör satırı
> `SELECT … FROM public.transactions` idi, yani **ölçtüğü şeye bağlıydı**. Ölçüldü —
> `REVOKE SELECT ON transactions` ile oturum **açılıyor**, psql **bağlanıyor**, ve yine
> *"Fix the connection first"* diyordu. Artık ilk ifade **`SELECT 'PROBE-RAN'`** (hiçbir
> tabloya, politikaya, yetkiye dokunmuyor) ve **üç ayrı** sonuç var: oturum hiç
> açılmadı · açıldı ama tabloyu okuyamadı · gerçek RLS hükmü. İkisi de ölçüldü.
> **B2** `cryptdecode --reverse … 2>/dev/null` + çıkış kodu kontrolsüz — B1'in kendi
> gerekçesi 70 satır sonra uygulanmamıştı. rclone'un cümlesi artık mesaja giriyor.
> **B3** 🔴 **İDDİA 16'YI YANLIŞ ÖLÇMÜŞÜM.** *"`base64 -w0` bu makinede çalışıyor"*
> demiştim; **stdin** biçimini ölçmüştüm, README ise **dosya argümanı** kullanıyor.
> Gerçek: `base64 -w0 <dosya>` → **`invalid argument`, rc=64, STDOUT BOŞ**, yani
> `| gh secret set KUBE_CONFIG` **BOŞ BİR SECRET** yazardı. Portatif biçime çevrildi ve
> iki lezzette **aynı bayt** olduğu md5 ile doğrulandı.
> **B4** Kanarya, işin sahip olduğu önekin **dışına** (crypt kökü) yazılıyor ve silme
> `|| true` idi; artık başarısız silme **uyarı basıyor** ve dosyanın nerede kaldığını
> söylüyor. ⚠️ Silme hatası **üretilemedi** → doğrulanamadı, ve öyle yazıldı.
> **B5** Sabit kanarya adı + elle yaratılan Job'lar yarışabiliyordu (`Forbid` yalnız
> denetleyicinin Job'larını kapsar). Ad artık **koşu başına benzersiz** — yarış
> belgelenmedi, **kaldırıldı**.
> **B6** `[ "$live" -gt 0 ]` boş değişkenle **hata** verir ve `&&` zinciri tabloyu
> *"blind"* listesine **almaz** — fail-open. Sayısal guard eklendi.
> **B7** `psql … | sort -u > have.grants` borusu: psql düşse durum `sort`'un (0) olur,
> dosya boş kalır ve `comm` **45 yetkinin hepsini "MISSING"** diye raporlardı. Her
> sorgu artık durumu kontrol edilen bir dosyaya iniyor.
> **B8** `obfuscate` geçiyor — **sayılmış seçim** olarak sınır 19'a yazıldı: rclone'un
> kendi belgesine göre şifreleme değil geri çevrilebilir karıştırma; kalan maruziyet
> **yalnız adlar**, içerik bayt kontrolüyle şifreli.

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
