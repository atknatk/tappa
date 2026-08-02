# ADR 0007 — Zamanlama guardrail'leri güvenlik uyarısını ön-alamaz

- **Durum:** kabul edildi
- **Tarih:** 2026-08-02
- **Karar veren:** yapıcı ajan, [M5-10](../plan/m5-tap-akisi.md#m5-10--tap-tazeliği)
  4. denetim turunda `tappa-security-auditor`'ın ölçtüğü regresyon üzerine
- **Etkilenen:** `internal/policy` (guardrail sırası) · [ADR 0004](0004-policy-motoru-modeli.md)
  §5 (guardrail sırası normatiftir) · [CLAUDE.md](../../CLAUDE.md) §5 satır 4 ·
  **[M3-05](../plan/m3-policy-motoru.md) — sıra tablosunun kendisi** (kartın
  normatif tablosu 4–7 bandında geçersizleşti; kartta **2026-08-02 düzeltme
  bloğu** olarak yürürlükteki tablo duruyor) · **[M3-08](../plan/m3-policy-motoru.md)
  — "M3-05'teki 1→10 sırası tablo bazlı doğrulanıyor" kabul kriteri** (artık
  düzeltme bloğundaki listeyi doğruluyor; ikinci bir kriter eklendi) ·
  [M6-09](../plan/m6-dashboard.md) (dashboard sıra ekranının atfı)

## Bağlam

[CLAUDE.md](../../CLAUDE.md) §5 satır 4: *"Çalışan `deactivated` → `reject` +
denemeyi logla + **güvenlik uyarısı**."* Üç yükümlülük, tek satır.

Motor tarafında bu üçü tek bir guardrail'de yaşar (`sys:employee-deactivated`):
`Effect` reddi verir, kaydı `internal/domain/checkin` yazar, uyarıyı guardrail'in
`Alert` fonksiyonu üretir. Ve `Alert` **yalnızca KAZANAN guardrail'den** okunur
([evaluate.go](../../internal/policy/evaluate.go)) — ADR 0004 §5'in "ilk eşleşen
terminaldir" kuralının doğrudan sonucu.

Bunun anlamı şudur: **`sys:employee-deactivated`'i ön-alan her guardrail §5 satır
4'ün üçte birini siler.** Ret gider, kayıt gider, **uyarı gitmez** — ve satır
doğru göründüğü için kayıp sessizdir. Genel üçüncü göz iki turda tam da bunu
kaçırdı.

## Ölçülen kusur

M5-10, `sys:tap-freshness`'in penceresini yapılandırılabilir yaptı (varsayılan
180 sn). Öncesinde pencere 900 sn'ydi ve bu, imzalı bağlamın TTL'ine **eşitti** —
yani guardrail hiç ateşlenemiyordu ve aynı istek `sys:employee-deactivated`'e
düşüyordu. M5-10 bandı açtı; band açılınca ön-alma **ilk kez gerçek oldu.**

Denetçinin sondası (gerçek Postgres, gerçek router, `-race`):

```
window=15m0s [2] DEACTIVATED, 5 dk bekledi -> 200, rows+1, verdict=reject,
             sid=sys:employee-deactivated   SECURITY ALERTS +1     ← M5-10 ÖNCESİ
window=3m0s  [2] DEACTIVATED, 5 dk bekledi -> 200, rows+1, verdict=reject,
             sid=sys:tap-freshness          SECURITY ALERTS +0     ← M5-10 SONRASI
```

**Saldırı yok, sadece BEKLEMEK var.** Ve senaryo gerçekçidir: deaktivasyon
oturumu bilinçli olarak iptal **etmez** (M5-01) — tam da bu denemenin
kaydedilebilmesi için.

### Aile taraması: #7'yi ön-alan diğerleri

Kusuru tek guardrail sanmamak için `employee:status = deactivated` sabit tutulup
ön-alabilecek her guardrail tek tek ölçüldü (`policy.Guardrails` + `tap.Decide`,
180 sn pencere). Sonuç, düzeltme öncesi:

| Ön-alan | `matched_sid` | Uyarı | Bilinçli mi? |
|---|---|---|---|
| `sys:tenant-mismatch` (1) | tenant-mismatch | — (redirect) | **Evet.** §4.5 / ADR 0002 Y2: kayıt **hiçbir** tenant'a yazılmaz. Uyarı da yazılamaz, çünkü yazılacağı tenant belirsizdir ve yazmak çapraz-tenant sızıntısıdır. |
| `sys:tag-not-active` (2), `lost` | tag-not-active | **var** (`lost-tag-tapped`) | **Evet.** Uyarı kaybolmuyor, adı değişiyor; kayıp plaket daha acil olaydır. |
| `sys:tag-not-active` (2), `retired` | tag-not-active | yok | **Kabul edildi, belgelendi.** Etiket durumu sunucu gerçeğidir, istemci seçemez; emekli plaket fiziksel olarak sökülmüştür. Kalan artık risk aşağıda. |
| `sys:sun-invalid` (3) | sun-invalid | yok | **Evet, ve KALIYOR.** Aşağıya bakınız. |
| `sys:tap-freshness` (4) | tap-freshness | **yok** | **HAYIR.** Regresyon. Bu ADR'nin sebebi. |
| `sys:occurred-at-bound` (5) | occurred-at-bound | **yok** | **HAYIR.** Daha keskin: girdi bir POST form alanı. |
| `sys:no-session` (6) | — | — | Erişilemez: `employee:status` anahtarı yalnızca oturum varken konur (`decide.go`), yani ikisi karşılıklı dışlayıcıdır. |

`sys:occurred-at-bound` ölçümü (`occurred_at` = `now + 60 sn`, sayfa taze):

```
05a_occurred_at_future_60s  verdict=reject sid=sys:occurred-at-bound SECURITY=false
05b_occurred_at_past_100h   verdict=reject sid=sys:occurred-at-bound SECURITY=false
```

Bu yol M5-10'dan **eskidir** ve maliyeti sıfırdır: `occurred_at`
[checkin handler](../../internal/handler/checkin.go)'ında opsiyonel bir form
alanıdır, yani deaktif bir oturum uyarının gönderilip gönderilmeyeceğini
**kendisi seçebiliyordu**.

## Karar

`sys:employee-deactivated`, §5'in adını **anmadığı** iki zamanlama
guardrail'inden **önce** gelir. Yeni normatif sıra:

| # | sid | Kaynak |
|---|---|---|
| 1 | `sys:tenant-mismatch` | §4.5 / ADR 0002 Y2 |
| 2 | `sys:tag-not-active` | §5 satır 1 |
| 3 | `sys:sun-invalid` | §5 satır 2 |
| 4 | `sys:no-session` | §5 satır 3 |
| 5 | `sys:employee-deactivated` | §5 satır 4 |
| 6 | `sys:tap-freshness` | ADR 0004 §11 / M5-10 |
| 7 | `sys:occurred-at-bound` | ADR 0004 §11 / K1 |
| 8 | `sys:person-debounce` | §5 satır 5 |
| 9 | `sys:policy-edit-owner-only` | ADR 0004 K2 |
| 10 | `sys:no-self-review` | ADR 0004 Y-C |

Değişen **yalnızca** 4–7 bandıdır: §5'in beş satırı artık §5'in kendi sırasında
durur — konumları `[2 3 4 5 8]`. ⚠️ **KESİNTİSİZ DEĞİL, ve bu ayrım önemlidir:**
yukarıdaki tablonun kendisinin gösterdiği gibi `sys:tap-freshness` (6) ile
`sys:occurred-at-bound` (7) hâlâ §5 satır 4 ile §5 satır 5'in **tam arasında**
duruyor. Kazanılan şey §5'e "kesintisiz uygunluk" değil, **uyarıdır**.

Neden bu ayrım: eski sıra da §5'in beş satırını **artan** konumlarda tutuyordu
(`[2 3 6 7 8]`), yani "göreli sıra korunmuş" ölçütünü **eski sıra da geçiyordu** ve
tam da regresyonu üreten sıra oydu. Dolayısıyla bu ADR'nin kararı bir sıralama
estetiği değil, tek bir ölçülebilir sonuçtur: **§5 satır 4'ün `Alert`'i hayatta
kalır.** Yerleştirme kuralının yürürlükteki, yanlışlanabilir hâli
[`internal/policy/guardrails.go`](../../internal/policy/guardrails.go)'nun
`Guardrails` doküman yorumundadır.

### `sys:sun-invalid`'in ön-alması KALIYOR — ve bu bir çelişki değil

`decide_test.go`'daki `sun_invalid_preempts_deactivated_alert` vakası o ön-almayı
bilinçli savunur: *"sahte bir SUN deaktif uyarısını imal edememeli (R8 bilgi
sızıntısı / push seli)."* Bu gerekçe **yerinde durur**, çünkü ayrım kuralın
istemciyle tetiklenebilir olması değil — sun-invalid de öyledir — **dokunuşun
kimliğinin doğrulanmış olup olmadığıdır:**

- **`sys:sun-invalid` yolunda dokunuş SAHTEDİR.** CMAC tutmamıştır. Kimliği
  doğrulanmamış bir istek yönetici bildirimi üretebilmemelidir; aksi hâlde çalıntı
  bir çerez + rastgele CMAC ile hem hesabın deaktif olduğu öğrenilir hem de
  bildirim seli üretilir.
- **Zamanlama yollarında dokunuş GERÇEKTİR.** `tap:sunValid = true`, CMAC
  doğrulanmış, sayaç ilerlemiş, etiket gerçek, oturum canlı. Sadece geç POST
  edilmiş ya da beyan edilen zaman damgası saçmadır. Ortada korunacak bir sahtelik
  yoktur; gerçek bir plaketin önünde duran gerçek bir deaktif çalışan **tam olarak**
  §5 satır 4'ün bildirilmesini istediği olaydır.

## Reddedilen alternatifler

**(A) `Decision.Security`'yi kazanan sid yerine `policy_context["employee:status"]`'ten
türetmek.** Denetçinin 2. seçeneği. En spesifik verdict'i **ve** uyarıyı birlikte
korurdu. **Ölçüldü ve elendi** — dokuz satırın dokuzunda da `SECURITY=true` üretti,
`03_sun_invalid` dâhil:

```
03_sun_invalid   verdict=reject sid=sys:sun-invalid  SECURITY=true   ← R8 ihlali
01_tenant_mismatch verdict=      sid=sys:tenant-mismatch SECURITY=true ← kayıtsız uyarı
```

Yani yukarıda gerekçesi verilen **bilinçli** sun-invalid bastırmasını kırdı
(`sun_invalid_preempts_deactivated_alert` kırmızıya döndü) ve hiçbir kayıt
yazmayan `tenant-mismatch` redirect'ine — hangi tenant'a gideceği belirsiz — bir
uyarı iliştirdi. Kurtarmak için "sid `sys:sun-invalid` ise hariç tut" tarzı bir
istisna gerekirdi; bu, `tap`'i tek bir guardrail'in adına bağlar ve `Security`'yi
kuralından koparırken kopmayı yeniden kurala bağlamaya çalışır.

**(B) Guardrail #4'ün `Match`'ine `employee:status == "active"` ön-koşulu eklemek.**
Denetçinin 3. seçeneği, en dar müdahale. Elendi: bir guardrail'in *koşulu*
başka bir guardrail'in *konusunu* okumaya başlar — sıralamayla ifade edilmesi
gereken bir öncelik, `Match` gövdesine gizlenmiş olur ve `Guardrails()` slice'ı
"sıranın tanımlandığı TEK yer" olmaktan çıkar (ADR 0004 §5). Ayrıca yalnızca
freshness'ı kapatır; `sys:occurred-at-bound` açık kalırdı ve `invited` durumu için
de yanlış cevap verirdi.

**(C) TTL'i yükseltip bandı genişletmek / freshness'ı kaldırmak.** Konu dışı:
sorun bandın genişliği değil, band içindeki kararın hangi kuralla adlandırıldığı.

## Sonuçlar

**Kazanılan.** §5 satır 4'ün üç yükümlülüğü **imzalı bağlamın TTL'i içindeki**
her zamanlama koşulunda ayrılmaz: sayfa ne kadar bayat olursa olsun (15 dk'ya
kadar) ya da `occurred_at` ne beyan edilirse edilsin, deaktif bir oturumun
denemesi `reject` + kayıt + **uyarı** üretir. TTL'in **ötesinde** üçü de yoktur —
garanti edilmeyen 5. madde. Uyarı seyreltmesi de kapanır — `sys:tap-freshness`
yüksek hacimli **iyi huylu** bir ret sınıfıdır (yavaş telefon, ekranı okuyan
kullanıcı) ve düşük hacimli kötü niyetli olayın onun içinde kamufle olması,
kaybın kendisinden daha uzun ömürlü bir sorundu.

**Verilen.** `matched_sid` artık daha az spesifiktir: 300 sn'lik bir sayfayla
gelen deaktif deneme `sys:employee-deactivated` der, `sys:tap-freshness` demez.
Tazelik gerçeği **kaybolmaz** — `policy_context` donmuş girdi anlık görüntüsünde
`tap:pageAgeSeconds = 300` olarak durur (migration 0008), yani satır kendini
tam olarak açıklayabilir. Ölçüldü ve teste bağlandı.

**Değişen davranış (kayıp yok).** **İKİ zamanlama kuralı da** `sys:no-session`'ın
arkasına geçti — yalnızca `occurred-at-bound` değil. Eski sırada `tap-freshness`
**4**, `occurred-at-bound` **5**, `no-session` **6** idi; ikisi de o üçlünün önünden
arkasına taşındı ve `guardrails.go`'daki yer değiştirmenin büyük kısmını
`sys:tap-freshness` bloğu oluşturuyor. Sonuç her ikisinde aynı: oturumsuz + bayat
sayfa, ya da oturumsuz + bozuk `occurred_at` isteği, kayıtlı bir `reject` yerine
§5 satır 3'ün aktivasyon yönlendirmesini alır. Bu §5'e **daha** uygundur (kimseyi
adlandırmayan bir satır yazmaktansa) ve HTTP yolunda zaten erişilemezdi: `Checkin`
kimliği bağlamı ayrıştırmadan önce çözer.

**Garanti EDİLMEYENLER.**

1. **`sys:tag-not-active` (`retired`) hâlâ ön-alır.** Emekli bir plakete dokunan
   deaktif çalışan uyarı üretmez. Kabul edildi: etiket durumunu istemci seçemez,
   emekli plaket sökülmüştür ve `lost` zaten kendi (daha acil) uyarısını verir.
   Kapatılmak istenirse ayrı bir karar gerekir — bu ADR onu kapatmaz.
2. **`sys:tenant-mismatch` hâlâ ön-alır ve hiçbir şey yazmaz.** ADR 0002 Y2'nin
   bilinçli sonucu; §4.5 uyarıdan önce gelir.
3. **Bu ADR uyarının TESLİM EDİLDİĞİNİ söylemez.** `tap.security_alert` bir
   `audit_log` satırıdır; yöneticiye gerçek push M6/M7'nin işidir. Burada
   garanti edilen tek şey satırın **yazıldığı**.
4. **Sıra elle bakımlıdır — ama artık mekanik bir ağı vardır.** Ölçüt
   [`guardrails.go`](../../internal/policy/guardrails.go)'nun `Guardrails` doküman
   yorumundadır: "§5'in beş satırının göreli sırasını koru" biçimindeki eski ölçüt
   2026-08-02'de **elendi**, çünkü ölçüldü — regresyonu üreten sıra da onu
   sağlıyordu (`[2 3 6 7 8]` ↔ `[2 3 4 5 8]`, ikisi de artan). Yerine geçen ölçüt
   **uyarı hayatta kalması** üzerinden yazılıdır ve
   `TestGuardrails_NothingUnnamedPreemptsAnAlert` ile **konumsal bir değişmez**
   olarak koşar: `Alert != nil` olan her guardrail'in önündeki her guardrail
   adlandırılmış istisna listesinde olmalı. Değişmez bir **yeniden sıralamayı**
   da bir **eklemeyi** de yakalar; sabit liste yalnız ilkini yakalıyordu ve
   ikincisinde doğal onarım (listeyi güncellemek) tam olarak yanlış hamleydi.
   Aynı gün ölçüldü — bugünün sırası geçiyor · eski sıra kırmızı ve
   `sys:tap-freshness` (konum 4) ile `sys:occurred-at-bound` (konum 5) **adıyla**
   suçlanıyor · `sys:employee-deactivated`'in önüne konan sahte bir 11. guardrail
   (`sys:counter-jump`) `TestGuardrails_NormativeOrder`'ın listesi güncellense
   bile kırmızı kalıyor — o mutasyonda `internal/domain/tap` ve
   `internal/handler` **yeşil**, `internal/policy`'de kırmızı olan tek şey bu
   değişmez. Kalan elle bakım payı: istisna listesini **küçültmek** hâlâ
   mümkündür — ama görünür ve tartışılır bir edittir, sessizce atlanamaz.
5. **TTL ötesinde §5 satır 4'ün üçü de yoktur.** İmzalı tap bağlamı 15 dk sonra
   süresi dolmuş sayılır (`tapContextTTL`, [tapcontext.go](../../internal/handler/tapcontext.go));
   guardrail'ler hiç danışılmadan istek reddedilir. Ölçüldü: deaktif bir oturum
   15 dk bekleyip taptığında `GET /t → 400`, `transactions +0`, `audit_log`'da
   **hiçbir** aksiyon — tenant tarafında sıfır iz. Bu bir regresyon **değildir**
   (M5-10 öncesi pencere 900 sn = TTL olduğundan band zaten aynıydı; düzeltme
   bekleme süresini 3 dk'dan 15 dk'ya çıkararak kesin olarak iyileştirdi), ama
   asimetri gerçektir: kapatılan yol satırı **yazıyordu**, kalan yol hiçbir şey
   yazmıyor. Yani *"deaktif çalışanın denemesi kaydedilir"* yükümlülüğü hâlâ
   beklemekle atlatılabilir. Sahibi M6/M7'nin uyarı teslimatıdır; TTL bandının
   iki tavanı [`TestCheckinDB_TwoCeilingsBoundATapPageAndTheyAnswerDifferently`](../../internal/handler/checkin_db_test.go)
   ile pinlidir.

## Kanıt

Üç katmanda, ikisi mutasyonla:

- `internal/policy` — `TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert`
  (**beş** vaka: bayat sayfa, gelecek `occurred_at`, çok eski `occurred_at`,
  **kişi debounce'u** — §5'in andığı ama aynı istekte ateşleyen üçüncü kural,
  2026-08-02'de eklendi — **ve** sahte SUN'ın hâlâ ön-aldığı karşı-örnek) +
  `TestGuardrails_NormativeOrder` + **konumsal değişmez**
  `TestGuardrails_NothingUnnamedPreemptsAnAlert`, iki negatif kontrolüyle
  (`…RejectsTheRegressionOrder`, `…RejectsAnUnnamedEleventhGuardrail`) —
  ilk ikisi bir **yeniden sıralamayı** yakalar, üçüncüsü bir **eklemeyi**.
- `internal/domain/tap` — `TestDecide_DelegatesOrderToPolicy` içinde
  `freshness_does_NOT_preempt_deactivated_alert` ve
  `occurred_at_bound_does_NOT_preempt_deactivated_alert`; ilki, bilinçli
  bastırmayı savunan `sun_invalid_preempts_deactivated_alert`'in **ikizidir** ve
  ikisi yan yana durur. `policy_context`'te `tap:pageAgeSeconds = 300`'ün
  kaldığını da doğrular.
- `internal/handler` — `TestCheckinDB_DeactivatedAlertSurvivesTheTimingGuardrails`
  gerçek Postgres + router üzerinde `tap.security_alert` satırını **sayar**
  (denetçinin sondasının kalıcı hâli).

**Mutasyon ölçümü — YÖNTEM** (tek yöntem olsun diye yazılı; önceki sürümde
"4 sn + 7 sn" / "11 sn" yazıyordu ve hangi komutun ürettiği yazılmıyordu, o yüzden
hepsi 2026-08-02'de yeniden ölçüldü):

1. Mutasyon `internal/policy/guardrails.go`'nun slice literalinde bir guardrail
   **bloğunu yerinden oynatarak** uygulanır (`git` kullanılmaz: dosya scratchpad'e
   kopyalanır, mutasyon yazılır, ölçüm alınır, **kopyadan geri yazılır**).
2. `.env` yüklenir (`set -a; . ./.env; set +a`) — yoksa `internal/handler`'ın DB
   testleri sessizce SKIP eder ve mutasyon yeşil görünür.
3. Paketler **ayrı ayrı** koşulur:
   `go test -race -count=1 ./internal/policy/ ./internal/domain/tap/ ./internal/handler/`
4. Süre, `go test`'in **paket başına** bastığı süredir. Toplam vermek yanıltıcı:
   üç paketin süresi aynı büyüklükte değil, `internal/handler` DB'ye bağlanır ve
   tek başına toplamın ~%97'sidir.

| Mutasyon (#5'in önüne al) | policy | tap | handler |
|---|---|---|---|
| `sys:tap-freshness` | 🔴 `NormativeOrder` + `…/stale_page` — 2,0 sn | 🔴 `…/freshness_does_NOT_preempt_deactivated_alert` — 0,9 sn | 🔴 `…/stale_page` — 94,9 sn |
| `sys:occurred-at-bound` | 🔴 `NormativeOrder` + `…/occurred_at_in_future`, `…/occurred_at_too_old` — 0,9 sn | 🔴 `…/occurred_at_bound_does_NOT_preempt_deactivated_alert` — 0,7 sn | 🔴 `…/occurred_at_in_future`, `…/occurred_at_far_past` — 93,8 sn |
| `sys:person-debounce` | 🔴 `NormativeOrder` + `…/person_debounce` — 1,0 sn | ⚪ **yeşil** — 1,7 sn | ⚪ **yeşil** — 94,3 sn |

Üç mutasyonda da `sun_invalid_preempts_deactivated_alert` (tap) ve
`forged_sun_still_preempts` (policy) **yeşil kaldı** — yani testler ön-almanın
kendisini değil, **hangi** ön-almanın meşru olduğunu ölçüyor.

⚠️ **Üçüncü satır bir kapsam gerçeğidir, bir regresyon değil.**
`sys:person-debounce` bugün #8'dedir, yani zaten arkadadır ve §5 onu **anar** —
bu ADR'nin düzelttiği iki kuraldan biri değildir. Ama aynı istekte ateşler
(deaktif çalışan + 10 sn önce tap) ve öne alınırsa **aynı sınıf kusuru** üretir:
`matched_sid = sys:person-debounce`, `SecurityAlert = ""` — üstelik effect
`deny` değil **`ignore`**, yani satır §4.6 anlamında yazılır ama ret bile görünmez.
Ölçüm: ADR'nin ilk yazımında `TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert`
üç ön-alabilen kuraldan **ikisini** kapsıyordu; bu mutasyonu yakalayan tek şey
`TestGuardrails_NormativeOrder`'ın **sabit listesiydi** — yani tam olarak
"Garanti EDİLMEYENLER" md. 4'ün elle bakım uyarısı. 2026-08-02'de o teste
`person_debounce` vakası eklendi; tap ve handler katmanlarında bu mutasyon **hâlâ
yeşil kalıyor** ve bilinçli olarak öyle bırakıldı: karar `internal/policy`'nin
sırasıdır, üst katmanlar onu delege eder.

## Bağlantılar

- [ADR 0004](0004-policy-motoru-modeli.md) §5 — guardrail sırası normatiftir.
  Bu ADR o maddeyi **değiştirmez**, içindeki sıra tablosunu günceller: 0004 §5'in
  iki `sys:sun-invalid` kısıtı aynen geçerlidir, üçüncü bir kısıt eklenmiştir.
- [ADR 0006](0006-debounce-iki-kosullu-zaman.md) — aynı desen: istemcinin
  etkileyebildiği bir zaman değeri bir güvenlik kararını taşıyordu.
- [CLAUDE.md](../../CLAUDE.md) §5 satır 4, §4.6.
