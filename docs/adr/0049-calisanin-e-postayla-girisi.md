# ADR 0049 — Çalışanın iş e-postasıyla girişi

- **Durum:** Önerildi (2026-11-18) — kabul M16-34'te, kullanıcının. Bu belge kullanıcının **bir** açık kararını kaydeder
  (aşağıda *"Karar veren"*); geri kalan her şey **öneridir** ve Q44/Q45 cevaplanmadan kesinleşmez.
- **Tarih:** 2026-11-18 · **Dal:** `plan/m16-mobil` (yalnız belge) · **Plan:** [docs/plan/m16-mobil.md](../plan/m16-mobil.md)
  → *Faz G* (M16-34, M16-35, M16-36)
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-18, ana oturumun AskUserQuestion'ıyla: giriş *"sign in with your work
  email"*dir — çalışan iş e-postasını yazar, e-postayla tek kullanımlık bir kod/link alır, cihaz bağı yine işverenin
  plaketine **ilk gerçek NFC dokunuşunda** tamamlanır; **kendi kendine kayıt yok** (hesabı işveren açar); yalnız **kayıtlı
  e-postası olan aktif** çalışanlar; oran sınırlı; **hesap numaralandırması yok** (adres kayıtlı olsa da olmasa da aynı
  cevap). Kullanıcı bunun bir güvenlik sınırını değiştirdiğini ve ADR istediğini de söyledi.
- **İlgili:** ADR 0002 md. 7 (`tappa_resolver`) · ADR 0005 (kabul edilen riskler) · ADR 0010 (deaktivasyon) · ADR 0022
  (işlemsel e-posta: §6 istek yolu dışı gönderim, §7 davet, §8 şablon, §9 oran, §10 log) · ADR 0025 (askı) · ADR 0026
  (aktivasyon ilk NFC dokunuşunda) · ADR 0028 §9/§13 (davet kuyruğu K-15/K-17/K-19) · ADR 0031 (panel kimliği) · ADR 0045
  (native uygulama; §2.3 cihaz anahtarı, Q40) · CLAUDE.md §4.4, §4.5, §5 *"Aktivasyon dokunuşu"*, §6, §7 · open-questions
  Q11, Q13, Q40, Q44, Q45.
- **Atıf kuralı:** `file:line` atıfları `origin/main` `6ab423eb`'ye göredir. **Okundu** = dosyadan okundu, çalıştırılmadı.
  **Doğrula** = bu belgeyi yazan doğrulayamadı; M16-35 ölçer ve sonucu buraya not olarak yazar.

---

## 1. Bağlam — bugün (okundu)

1. **Aktivasyonu yalnız bir davet başlatır ve daveti yalnız işveren tarafı basar.** `invite.Manager.IssueAndDeliver`
   davet yaratmanın *"ONLY exported way"*idir (`internal/invite/manager.go:218-220`); çağıranları panelin müdür eylemi ve
   M11'in kuyruğudur (`internal/invite/queue.go:3-6`: API'nin açtığı çalışan, adres değişikliği, *resend activation*).
   Çalışanın kendisi davet isteyemez: yeni telefon, silinen çerez (Q11; 2026-11-17 *"Finish setup"* vakası) ya da
   kaldırılıp yeniden kurulan uygulama → müdürden yeniden davet.
2. **Davet e-postayla gider** — üretim `TAPPA_INVITE_DELIVERY: "email"` (`deploy/k8s/05-config.yaml:166`). Yani bugün de
   aktivasyon linki çalışanın kayıtlı posta kutusuna düşer; değişen yalnız **kimin bastığıdır**.
3. **Davetin kuralları olgundur:** 128 bitlik kod, yalnız HMAC'i saklanır (`00009`), tek kullanımlık; basım kişinin öteki
   bekleyen davetlerini aynı transaction'da emekliye ayırır (`manager.go:237-254`, kullanıcı kararı 2026-08-08 — iki geçerli
   link bir hesap ele geçirmesiydi); ADR 0022 §9 sınırları (`internal/invite/email.go:75-79`: işletme saatte 50, günde 300;
   kişi saatte 3) ve posta kutusu tavanı (`mail.RecipientCap`, EM-7C: işletme × posta kutusu başına saatte 5, günde 20);
   süreç kesicisi; ADR 0022 §7'nin *"aynı tenant'taki bir yönetici adresine eşit adres"* reddi. Süre `minTTL` 1 saat …
   `maxTTL` 30 gün (`manager.go:133-135`).
4. **Tamamlanma ADR 0026'nındır** ve uygulamada aynı sıradır (ADR 0045 §2.3, CLAUDE.md §5): onay + bağ → ilk gerçek NFC
   okuması → ilerletmeyen ön kontrol (işverenin aktif, monte plaketi) → `sun.Verify` (CMAC, atomik `ctr`) → tek ifadeli
   tüketim → oturum/cihaz; kişinin öteki kimlik bilgileri aynı transaction'da iptal (Q40 (a)).
5. **`employees.email` tenant içinde tekildir, global değil ve boş olabilir** (`00003_create_employees_sessions.sql:48-53`,
   `:83-84`): aynı adres birden çok işletmede çalışan olabilir. Sütun `citext`tir ve sabit `search_path`'li bir bağlamda
   `citext` eşitliği **sessizce** düz metin eşitliğine düşer; `00011:295-313` bunu ölçtü ve *"the FIRST person to write
   `WHERE email = ...` over employees … inherits this trap"* diye uyardı. Bugün `employees.email` üzerinde arama yapan sorgu
   yoktur (okundu: aynı not).
6. **Kimliği doğrulanmamış, adresle başlayan bir istek için emsal vardır:** `POST /admin/reset` — her durumda aynı cevap,
   250 ms taban süre (`resetRequestFloor`, `internal/handler/adminresetlimits.go:347`; taban işlevi
   `internal/handler/adminreset.go:1640-1655`), gönderim istek yolunun dışında (ADR 0022 §6), adres log'a yazılmaz
   (ADR 0022 §10). Adresten hesap çözümü `resolve_admin_by_email`'dir (`00011`, `tappa_resolver`).
7. **Neden şimdi:** App Store Review Guidelines 2.1(a) giriş içeren uygulama için inceleme hesabı ister; uygulama içinde
   daveti olmayan birine hiçbir yol göstermeyen bir giriş ekranı ret riskidir. Uygulama hesap **yaratmaz** (3.1.3(c),
   işveren açar; 5.1.1(v)'nin *"account creation"* koşulu doğmaz), 4.8 gereği Sign in with Apple gerekmez. (Numaralar ana
   oturumun 2026-11-18 okumasındandır — **doğrula** M16-40.)

## 2. Karar (öneri; kullanıcının kararı yukarıda)

### 2.1 Kim isteyebilir

- Yalnız `employees.status = 'active'` **ve** `email` dolu satırlar (kullanıcı kararı). Davetli (`invited`) kişi bu yoldan
  bir şey almaz: e-posta modunda müdürün daveti zaten aynı posta kutusundadır; deaktive kişi hiçbir şey almaz (ADR 0010).
- ADR 0022 §7'nin kuralı aynen: adres aynı işletmedeki bir yönetici adresine eşitse gönderim yok (müdür çalışanın linkini
  alamasın).
- Askıdaki işletme (ADR 0025) davet basabiliyorsa aynı kural, basamıyorsa bu yol da basmaz — **doğrula** (M16-35).
- Aynı adres birden çok işletmede aktifse **her işletme kendi davetini** kendi tenant bağlamında ve kendi e-postasıyla
  gönderir (e-posta işletmenin adını taşır); bir istek en çok N işletmeye dokunur (öneri 5).

### 2.2 Tek cevap

- Uç (öneri `POST /app/v1/signin/request`, imzasız) her durumda **aynı durumu, aynı gövdeyi ve aynı taban süreyi** verir:
  kayıtlı değil · davetli · deaktive · yönetici adresi · kişi/posta kutusu/işletme sınırı · kuyrukta bekleyen istek.
  Taban `POST /admin/reset`'inkidir (250 ms); süre dağılımı M16-35'te ölçülür.
- **Tek istisna** kaynak adres kovasıdır (süreç içi limiter; reset yolunun B9 emsali): aşımı 429 + `Retry-After`dır.
  Adresten bağımsız olduğu için kehanet değildir.
- Eşleşmeyen adres **hiçbir** iz bırakmaz (tenant'ı yoktur; `audit_log` tenant kapsamlıdır) ve **hiç** e-posta almaz —
  tanımadığımız adreslere posta göndermek bizi bir posta bombası aracına çevirirdi.
- Gönderim istek yolunun **dışındadır** (ADR 0022 §6'nın gerekçesi: senkron SMTP tabanı aşar ve cevabın süresi adresi ele
  verir).

### 2.3 Gönderilen kimlik bilgisi — Q44

Seçenekler ve öneri [open-questions Q44](../plan/open-questions.md)'tedir. **Öneri: (a) taze bir davet.**

- **(a)'nın biçimi:** istek, eşleşen her `(tenant, çalışan)` için K-17 davet kuyruğuna (`invite_requests`, `00037`) bir
  satırdır; kod **gönderim anında** basılır, kuyrukta kod yoktur (`queue.go:11-14`); limitler panelinkiyle **aynı
  sayaçlardır** (`queue.go:24-27`); K-19 gereği işçi yalnız işi olan tenant'ı okur (tenant'lar arası tarama yok). Kuyruğun
  K-15 kuralı (*"ONLY AN 'invited' PERSON IS SENT ONE"*, `queue.go:28-32`) **bu istek türü için** *"yalnız `active`"* olur;
  ayrım bir istek türüyle yapılır (00116 — `invite_requests`'e tür; değerler ve CHECK M16-35'te). Kuyruğun aynı kişi için
  tek satır kuralı (birincil anahtar `(tenant_id, employee_id)`, `00037:189`) aynı kişiden gelen ardışık istekleri kendiliğinden
  birleştirir — çakışmada ne olacağı (tür üstüne yazılır mı, bekleyen *resend* korunur mu) M16-35'in ölçüsüdür.
- **Süre:** `minTTL` (1 saat). Daha kısası `minTTL` değişikliği ve bu ADR'ye not ister.
- **Neden (a):** yeni bir kimlik bilgisi türü yok, yeni tablo yok, deneme kilidi gerekmez (kod ≥ 128 bit; `00009`'un
  notu), davetin olgun kuralları (emekliye ayırma, sınırlar, tavan, yönetici
  adresi, audit, sızıntı testleri) **aynen** devralınır. Bedeli §4'te.
- **(b) kısa kod** seçilirse: yeni tablo (§6 beşlisi, en dar GRANT, RLS izolasyon testi), kodun yalnız HMAC'i, kod başına
  deneme sayacı ve katı kilit, isteği yapan cihaza bağlılık; doğrulanınca sunucu içeride (a)'nın davetini basar ve bir
  `Channel` uygulaması linki yalnız o yanıta verir — `IssueAndDeliver`'ın *"tek çıkış"* dikişi korunur.

### 2.4 Tamamlanma değişmez

Bu ADR aktivasyonu **başlatan** şeyi genişletir, **tamamlayan** şeyi değiştirmez: ilk gerçek NFC dokunuşu, işverenin aynı
tenant'taki aktif plaketi, `sun.Verify`'ın atomik `ctr`'ı (CLAUDE.md §4.4), tek ifadeli tüketim, Q40 (a). Bu yolla
basılmış bir davetin tüketimi `transactions` satırı **yazmaz** (ADR 0026). Faz D'nin NFC'siz yolları (M16-23) bu davete de
uygulanır, başka hiçbir şey değişmez.

### 2.5 Tarayıcı — Q45

Öneri: aynı sunucu ucu tarayıcının aktivasyon ailesinde de sunulur (Q45 (a)); ekran ekleme CLAUDE.md §9 kararıdır.

### 2.6 Log ve audit

- Adres, adresin özeti, kod, link hiçbir log satırında, hata metninde ve `audit_log` `detail`'inde yoktur (CLAUDE.md §7;
  ADR 0022 §10'un kapalı anahtar kümesi). Eşleşen işletmenin `audit_log`'una kuyruğun bugünkü satırları (`invite_id`,
  `employee_id`, kanal) ve kaynağın `signin` olduğu yazılır.
- Redline R7'nin Go kolu yeni handler dosyasında `email`'i tanır (gürültü önce ölçülür).

## 3. Güvenlik değerlendirmesi

| Tehdit | Bugün | Bu ADR'yle | Kalan |
|---|---|---|---|
| Hesap numaralandırma | yol yok | tek cevap + taban süre; eşleşmeyene e-posta yok | kaynak kovası 429'u adresten bağımsız; e-postanın **gelişi** yalnız posta kutusu sahibine bilgi verir |
| Posta kutusu ele geçirilmiş | müdürün e-postayla gönderdiği davet aynı kutuya düşer; saldırgan müdürün basmasını bekler | saldırgan kendisi bastırır | tamamlamak için **işverenin plaketine fiziksel dokunuş** gerekir (SUN, atomik `ctr`); tamamlanınca gerçek çalışanın telefonu çıkış yapar (Q40 (a)) — fark edilir. Adlandırılmış bedel; ADR 0005'e ek önerisi (M16-40) |
| Posta bombası | — | kişi saatte 3, işletme × posta kutusu saatte 5 / günde 20, işletme saatte 50 / günde 300, süreç kesicisi (~300/sa), istek başına ≤ N işletme | işletmeler arası toplam, adresi tutan işletme sayısıyla sınırlı (ADR 0022 EM-7C'nin bilinçli sınırı) |
| Taciz (bekleyen daveti öldürmek) | — | istek, kişinin bekleyen davetini emekliye ayırır | yeni link **aynı** kutuya düşer; çalışanın elinde her zaman en son geçerli link vardır |
| Tenant'lar arası okuma | çözümleyiciler sırdan çözer (oturum token'ı, plaket UID'si) | çözümleyici sır **olmayan** bir girdiden (adres) `(tenant, çalışan)` döndürür | emsali `resolve_admin_by_email` (`00011`); dönen yalnız kimlikler, cevap istemciye yansımaz, sonrası `WithTenant` + açık `tenant_id`. `citext` tuzağı (`00011:295-313`) üç yazımla ölçülür |

## 4. Bedeller

1. **Kopyala-yapıştır (Q44 (a)).** `/activate` Universal Link / App Link'e bağlı değildir (Q40, M16-08); e-postadaki link
   aynı telefonda tarayıcıyı açar ve tarayıcı aktivasyonunu başlatabilir. Uygulamayı kullanmak isteyen çalışan linki
   kopyalayıp uygulamanın bugünkü yapıştırma ekranına verir (M16-12/M16-16). Sürtünme pilotta ölçülür; (c) ayrı kartla.
2. **Bir saatlik link.** `minTTL` altına inmek bir değişiklik ister; bir saat, kısa kodun dakikalarından uzundur ama
   kodun 128 bitlik uzayı çevrim içi aranamaz.
3. **Adres bir kimlik doğrulayıcı olur.** Müdürün yazdığı adres artık yalnız bir teslim adresi değil, aktivasyonun
   başlangıcıdır: yanlış yazılmış bir adres (başka birinin kutusu) o kişiye başkasının hesabını **başlatma** imkânı verir
   — tamamlamak yine plakette fiziksel dokunuş ister. Bugünkü e-posta modunda müdürün daveti aynı yanlış kutuya zaten
   gidiyordu; değişen, isteğin müdürsüz tekrarlanabilmesidir.
4. **İşletme kapatamaz** (öneri: ilk sürümde işletme başına kapama yok). Bir müşteri isterse ayar ya da `base:` politikası
   ayrı karardır (*Karar verilmedi*).

## 5. Kırmızı çizgiler — madde madde

- **§4.1, §4.2:** dokunulmaz (biyometri yok, konum okunmaz).
- **§4.3, §4.6:** bu yol **kayıt yazmaz**; aktivasyonun tamamlanması da yazmaz (ADR 0026).
- **§4.4:** tamamlanma `sun.Verify`'ın atomik `ctr`'ından geçer — değişmez.
- **§4.5:** adresten çözüm bir `tappa_resolver` fonksiyonudur (`00011` deseni); sonrası tenant bağlamında ve açık
  yüklemle; RLS izolasyon testi M16-35'te.
- **§4.7 + §7:** adres ve kod loglanmaz; kodun yalnız HMAC'i saklanır (`00009`).

## 6. Karar verilmedi

- Q44 (kimlik bilgisi), Q45 (tarayıcı).
- İşletme başına kapama (§4 madde 4).
- `invited` kişilerin de bu yolu kullanması (kullanıcı kararı: yalnız `active`; e-posta modunda gerekmez).
- `minTTL`'in altına inmek.
- ADR 0005'e *"posta kutusu + fiziksel dokunuş"* riski eki — öneri: evet, M16-40'ta, uygulamanın ölçümüyle.

## 7. Uygulama

- **M16-34:** bu ADR'nin kabulü; Q44, Q45; ADR 0022 ve ADR 0026'ya not.
- **M16-35:** sunucu — uç, çözümleyici (00116), kuyruk türü, tek cevap, sınırlar, log; CLAUDE.md §5 *"Aktivasyon
  dokunuşu"* cümlesi (kodla aynı commit).
- **M16-36:** iki uygulamanın ekranı ve Q45'e göre tarayıcı.
