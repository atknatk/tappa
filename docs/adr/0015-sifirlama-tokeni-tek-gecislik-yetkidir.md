# ADR 0015 — Sıfırlama token'ı tek geçişlik bir yetkidir, oturum değil

- **Durum:** kabul edildi
- **Tarih:** 2026-08-14
- **Bağlam:** M7-04 A fazı (veri katmanı), migration
  [00019](../../db/migrations/00019_harden_password_resets.sql)
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) md.7 (altıncı çözümleyici
  notu) · [ADR 0014](0014-parola-digestinin-tabani-islenebilirlik.md) (00018) ·
  [ADR 0005](0005-kabul-edilen-riskler.md)

## Neden ayrı bir ADR

Emsal iki yöne işaret ediyor ve karar gerekçesiyle yazılıyor. ADR 0002 §7 zaten
**iki** çözümleyici eklemesini not olarak taşıyor — biri normatif olarak bundan
büyük, çünkü bir kısıtı geçersiz kılıyor. Ama **00018 tek bir CHECK için kendi
ADR'sini aldı (0014)**, oysa 00019 şunları birlikte yapıyor:

1. `tappa_app`'in **aynı tenant içinde yetki yükseltmesini** kapatan bir GRANT
   yeniden yazımı,
2. iki CHECK (süre tavanı, hash şekli),
3. bir sütun (`cancelled_at`) ve onun kardeş-emekliye-ayırma anlamı,
4. **altıncı** SECURITY DEFINER çözümleyici.

Bir sonraki oturumun *"sıfırlama token'ı ne yapabilir, ne yapamaz"* sorusunu
`git log` kazmadan bulabilmesi gerekiyor. 0002 §7 notu **çözümleyici sayımının**
doğru evi; **yetki yükseltmesinin** evi burası.

## Karar

**1. Sıfırlama token'ı TEK BİR DURUM GEÇİŞİNE izin verir ve OTURUM VERMEZ.**
`password_resets` satırı yalnızca `admin_users.password_hash`'i bir kez yazdırır,
sonra ölür. Bu, M7-04 kartındaki *"magic link ve/veya şifre"* ayrık koşulunun
**daraltılmasıdır**: sağ kol M6-01'de sevk edildi, sol kol ikinci bir kimlik
doğrulama yoludur ve **hesap sahibine görünmez**. Çalınan sıfırlama linki kurbana
gördüğü bir parola değişikliğine mal olur (parolası çalışmaz, oturumları iptal
edilir, audit satırı vardır); çalınan magic link sessiz bir giriştir — §4.6'nın
reddettiği şekil. Magic link istenirse **kendi tablosunu ve kendi ADR'sini** alır;
`password_resets`'e bindirmek dağıtılmış her sıfırlama token'ını bir oturum
token'ına yükseltirdi.

**2. Yazılabilir sütun kümesi yetki düzeyinde daraltılır.** `UPDATE` yalnız
`(used_at, cancelled_at)`; `INSERT` yalnız
`(id, tenant_id, admin_user_id, token_hash, expires_at)` — `created_at` **bilerek
dışarıda**, çünkü süre tavanı CHECK'i bir **aralık** ölçer ve yazılabilir bir
`created_at` o aralığı geleceğe taşıyarak tavanı anlamsızlaştırırdı.

**3. Süre bir şema tavanıyla sınırlıdır** (`expires_at <= created_at + 24 saat`),
ürün TTL'i (`adminauth.ResetTTL` = 1 saat) bu tavanın altında bir Go sabitidir.
Veritabanı **asla olmaması gerekeni** reddeder; sabit, olabilecek aralıkta seçim
yapar.

**4. Çözümleme ADR 0002 md.7'nin çevrelenmiş bypass'ıdır**, çünkü reset linki
yalnız token taşır ve tenant aramanın **sonucudur**. Beş kısıt canlı katalogda
yeniden kazanıldı; ayrıntı ve reddedilen iki alternatif 0002 §7'nin 2026-08-14
notundadır.

**5. Tüketim ile oturum iptali TEK transaction'dadır.** Parolayı değiştirip eski
çerezleri yaşatan bir sıfırlama, kurbanın çaresinden **sağ çıkan** bir ele
geçirmedir.

## Ölçüm — kapanan şey ve kapanmayan şey

00019 **öncesi**, `tappa_app` olarak kendi tenant bağlamında, `BEGIN…ROLLBACK`
içinde (yük 7.71):

| # | İfade | Önce | Sonra |
|---|---|---|---|
| A1 | canlı token'ı **başka bir admin'e** yönlendir | `UPDATE 1` | `42501` |
| A2 | süreyi 10 yıl uzat | `UPDATE 1` | `42501` |
| A3 | `used_at = NULL` (tekrar oynat) | `UPDATE 1` | **`UPDATE 1`** |
| A4 | seçtiğim hash'i satıra yama | `UPDATE 1` | `42501` |
| A5 | `expires_at = 'infinity'` | `INSERT 0 1` | `23514` |
| A6 | gelecek `created_at` | `INSERT 0 1` | `42501` |
| A7 | `token_hash = 'plaintext-reset-token'` | `INSERT 0 1` | `23514` |

**A1 bir HESAP ELE GEÇİRME ilkeliydi ve RLS onu göremez** — aynı tenant. Kendisi
için sıfırlama isteyen bir *manager*, elindeki linkle **owner'ın parolasını**
yazabiliyordu.

### 🔴 Kapanmayanlar, adıyla

**(a) MINTING kapanmadı ve kapanamaz.** INSERT grant'ı zorunlu olarak
`admin_user_id` **ve** `token_hash` taşır — bir yönetici için, uygulamanın
hesapladığı bir hash altında sıfırlama **basmak** bu tablonun bütün işidir.
00019 **sonrası** ölçüldü:

```
N1  INSERT ... (admin_user_id = <OWNER>, token_hash = <benim seçtiğim>) -> INSERT 0 1
N2  <ConsumePasswordResetAndSetPassword'ın birebir şekli>               -> UPDATE 1
```

Yani kapanan fiil **re-pointing**, *minting* değil. Ele geçirmeyle arasında duran
şey SQL değil, ham token'ın **yöneticinin kendi satırındaki adrese** teslim edilip
çağırana asla döndürülmemesidir — bu **B fazının yükümlülüğüdür**.
⚠️ Kıyas: `employee_invites` hâlâ tablo geneli INSERT tutuyor (`tappa_app=ar`),
yani `password_resets` bu yönden **daha dar**.

**(b) A3 (değer geçişi) yapısal değil.** Sütun grant'i hangi sütunun
yazılabileceğini söyler, hangi **değeri** değil. Koruma `db/queries` disiplinidir;
yapısal hâle getirmek bir trigger ister ve o da `employee_invites`'ın aynı
sorununu birlikte cevaplamalıdır (00009/00012 aynı sınırı kaydediyor).

**(c) 🔴 MINTING'İN UZUN YOLU DEĞİL, KISA YOLU: `tappa_app` `admin_users` üzerinde
DOĞRUDAN `UPDATE` tutuyor.** (a) doğru ama **yanlış sınırı adlandırıyordu**.
00017 o tabloda sütun-düzeyi
`UPDATE (full_name, email, password_hash, role, status, last_login_at)` veriyor ve
ölçüldü: `UPDATE admin_users SET password_hash = … WHERE id = <OWNER>` → **`UPDATE
1`**, üstelik `SET LOCAL app.tenant_id` ile **istenen tenant** adlandırılarak. Yani
bu ADR'nin koruduğu şey sıfırlama **akışıdır**, yönetici hesabı değil: gerçek artık
risk **`tappa_app` olarak koşan herhangi bir SQL = her tenant'ın her yönetici
hesabı**.

**NEDEN BURADA, ADR 0005'TE DEĞİL** (karar ve gerekçe): 0005 *kabul edilen ürün
risklerinin* kaydı, bu ise **bu ADR'nin iddiasının sınırıdır** — "sıfırlama token'ı
tek geçişlik bir yetkidir" cümlesi, o yetkinin **yanından dolaşılabildiği** bilgisi
olmadan fazla güçlü okunur. Zaten var olan ev `db/queries/admins.sql`'in başlığıdır
(borcu adıyla M6-05 ve M7-04'e yazıyor); buradaki madde onu **bu kararın kapsam
cümlesine** bağlar. Kapatmak `tappa_app`'in panel yönetimi yeteneğini yeniden
tasarlamayı ister (rol muhafızı ya da tetikleyici) ve M6-05/M7-05'in işidir.

**(d) Rakip emekliye ayırma = kurtarma reddi.** F1, aşağıda kabul edilen riskler
arasında.

## Kabul edilen riskler

- 🔴 **RAKİP EMEKLİYE AYIRMA BİR KURTARMA REDDİDİR — ve bu ADR onu kendi reddettiği
  alternatif 4'te tarif edip sevk edilen tasarımda zayıf sürümünü üretti.**
  `CreatePasswordReset` kardeşleri emekliye ayırdığı için, kurbanın linki
  postalanırken saldırgan aynı adresi genel forma yazarak onu **iptal ettirir**:
  `T1 retired_count=0` → `T2 retired_count=1` → `Consume(T1)` **0 satır**. Tekrar
  edilirse hesap sahibi kurtarmayı **hiç tamamlayamaz**. Parolası değişmez (o
  yönden kilit yok), ama **kurtarma yolu** kapanır.
  **Neden yine de emekliye ayırıyoruz:** ayırmamak M6-05'in ölçtüğü zararı geri
  getirir (birinin gelen kutusunda canlı kalan eski link = hesap ele geçirme).
  **Sınırlayan şey:** istek uç noktasında **adres başına oran sınırı** — Faz B
  yükümlülüğü, M7-04 kartının kabul kriterlerine yazıldı. Veri katmanı bunu
  yapamaz: `Issue` uuid alır, adres almaz.
- 🔴 **`Consume` token'ın var olup olmadığına bakmadan tam bir cost-12 bcrypt
  öder** (ölçüldü: **212,8 ms**), ve sayfa kimlik doğrulamasızdır → yüz eşzamanlı
  istek kutuyu doyurur (M7-02'nin zarar sınıfı). **Sıra bilinçlidir ve
  değiştirilmez:** önce çözümlemek, bilinen token'ı bilinmeyenden **zamanlamayla**
  ayırır — kimlik doğrulamasız sayfada bir kehanet. Çare **aynı oran sınırıdır**.
- **Eşzamanlı iki `Issue` iki canlı token bırakabilir.** Sıralı hâl (insanın iki
  kez basması — M6-05'te ölçülen zarar) tamamen kapalı. Fiyat: çağrı yerinde
  kişi-başına advisory kilit (ADR 0006 şekli), B fazının kararı.
- **`token_hash` CHECK'i `NOT VALID`.** 6502 artık satır (tamamı test kirliliği)
  donmuş; **canlı ihlal eden satır 0**, etkilenen yönetici **0**, ve
  `expires_at` yazılabilir olmadığı için donmuş bir satır **canlanamaz**. Taze
  kurulumda sınıf hiç oluşmaz.
- **Şekil ≠ köken.** CHECK bir değerin biçimini zorlar, nereden geldiğini asla;
  HMAC türetmesi `internal/adminauth`'un yükümlülüğüdür.
- **Geliştirme Postgres'i hash'i log'a yazıyor** (`log_statement=all`); ölçüldü,
  sevk edilen bind-parameter yolunda 64-hex değer taşıyan 10 `Parameters:` satırı.
  Bu tasarımda hash bir **taşıyıcı** kimlik bilgisidir. Üretimde bu ayar yok;
  borç `docker-compose.yml`'e ait (00018 aynısını `password_hash` için kaydediyor).
- **`token_hash` yazma yolunda çıplak `string`.** Ham token'ın beş redaksiyon
  arayüzü var, **hash'in sıfır**: `store.*Params.TokenHash`/`.PasswordHash` sqlc
  üretimi düz alanlardır, yani bir `%+v` onları basar. Depo genelindeki desen budur
  (`CreateAdminUserParams.PasswordHash`, `CreateInviteParams.CodeHash` de çıplak),
  yeni bir sapma değil; Faz B yükümlülüğü tek cümle: **bir store `*Params` değerini
  asla yazdırma**.
- **Donmuş satırın owner çaresi hash'i sızdırabilir** (F3): aynı reddedilen INSERT
  `tappa_app`'te DETAIL'siz, `tappa_owner`'da `DETAIL: Failing row contains (…)`.
  Uygulama rolünde kapalı olmasının sebebi bu migration değil **FORCE RLS'in yan
  etkisidir** — RLS-muaf bir rolle koşan ilk yolda geri gelir. Prosedür 00019'un
  remedy paragrafında (`\set VERBOSITY terse`, log toplamayan oturum).
- **Başarısız bir sıfırlama denemesi bugün hiçbir yere yazılmıyor.** Çözümlenen
  token için `Consume` gerekli olguları hataya ek olarak döndürür (B fazı
  `audit_log`'a yazar); **çözümlenmeyen** token'ın tenant'ı yoktur ve
  `audit_log.tenant_id` NOT NULL'dur (00005) — o vaka ancak bir süreç log'u
  olabilir. §4.6'nın bu akıştaki kapsaması bir **B fazı yükümlülüğüdür**.

## Değerlendirilen ve reddedilen alternatifler

1. **Yeni bir `magic_links` tablosu / oturum veren token.** Reddedildi: karar 1.
2. **Tenant'ı reset linkine koymak** (çözümleyici yerine). Reddedildi: tenant
   çözümlemenin çıktısıdır; girdi kabul eden bir yol, çağıranın kendi tenant'ını
   adlandırmasına ve o transaction'daki her ifadenin orada koşmasına izin verir.
3. **Reset sayfasında e-postayı tekrar sorup `resolve_admin_by_email`'i yeniden
   kullanmak.** Reddedildi: (2)'nin gerekçesi tek başına yeterli.
   ⚠️ Bir ara *"aynı `MaxCandidates` kilidini miras alır"* diye ikinci bir gerekçe
   yazılmıştı; **ölçümle geri çekildi** — çözümleyicide `LIMIT` yok, `MaxCandidates`
   yalnız `Authenticate`'in bcrypt döngüsünde kullanılan bir Go sabiti.
4. **Sıfırlama isteğinde eski parolayı devre dışı bırakmak** (yaygın ama yanlış
   kalıp). Reddedildi: herkese açık bir formda adres yazan biri, herhangi bir
   yöneticiyi **kendi panelinden edebilirdi** — M7-02'nin bir kez sevk ettiği zarar
   sınıfı.
