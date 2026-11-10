# ADR 0043 — Plan geçmişi: bir faturalama ayı, başladığı andaki planla fiyatlanır

- **Durum:** Önerildi (2026-11-09), denetim bekliyor.
- **Tarih:** 2026-11-09 · **Backlog:** T27 (OP-K12) · **Dal:** `m14-planhist` · **Migration:** 00097 (tenant yarısı), 00098 (operatör yarısı)
- **Kullanıcı kararı (2026-11-08):** T27 için **plan geçmişi tablosu**. Ay kapanışı o ay yürürlükte olan planı okur ("subscription schedule" kalıbı).
- **İlgili:** CLAUDE.md §4.3 (append-only ailesi), §4.5 (tenant izolasyonu; `op_*` istisnası, ADR 0020/0021), §6 · ADR 0005 (T27 satırı) · ADR 0021 §1–§3 · 00016 (faturalama aritmetiği), 00032 (`op_read_tenant_billing`) · `docs/plan/m10-platform.md` OP-17 (bu karara ve T37'ye bloke).
- **Ölçüldü** = bu dalda, şerit veritabanı `tappa-m14-planhist-db` (PostgreSQL 17) ve aynı konteynerde sıfırdan kurulan `planhist_rt2`. **Okundu** = dosyadan, çalıştırılmadı.

## 1. Sorun

`PreviewBillingPeriod`, `CloseBillingPeriod` (`db/queries/billing.sql`) ve 00032'nin `op_read_tenant_billing`'i bir ayın `free_period`'unu `tappa_first_chargeable_month(tenants.plan, …)`'tan türetiyordu. Yani ayı **okunduğu andaki** plandan. Bitmiş ama kapatılmamış bir ay, kendisinden sonra yapılan bir plan değişikliğini izliyordu. `billing_periods` append-only olduğu için (00016) kapanış bu kaymış cevabı kalıcı olarak donduruyordu (okundu).

Bugün `tenants.plan`'ı yazan bir ürün yolu yok. Hiçbir migration bir role `UPDATE (plan)` vermiyor ve hiçbir fonksiyon gövdesi onu atamıyor (ölçüldü, şerit veritabanı: süper kullanıcı dışında hiçbir rolde sütun üzerinde UPDATE yok, `public`'teki hiçbir fonksiyon gövdesi `tenants`'ı `plan =` ile güncellemiyor; okundu: hiçbir migration bunu vermiyor). Yalnız `tappa_owner` elle yazabilir. Operatörün plan ekranı OP-17'dir ve bu karara bağlıdır.

## 2. Karar — kural

**M ayı, M'nin ilk anında yürürlükte olan planla fiyatlanır.** O an tenant'ın yerel ayının 1'i gece yarısının UTC karşılığıdır. Yani `billing_periods.period_from`'un dondurduğu sınır (`tappa_local_month_start(M, zone)`).

- X anında yapılan bir değişiklik, X'te ya da X'ten sonra **başlayan** ilk ayı yönetir. Kısacası "bir sonraki faturalama döneminden itibaren".
- Kayıt ayı, tenant var olmadan başlar. Onu tenant'ın başladığı plan yönetir (en eski satır).
- Başlamış bir aya, kapatılmış olsun olmasın, hiçbir değişiklik dokunamaz. T27'nin zararı tam olarak budur.
- Sınır yarı açıktır: `[period_from, period_to)`. Tam `period_from` anındaki bir değişiklik o ayı yönetir (`effective_from <= p_at`).

**Saat dilimi seçimi.** Ay, tenant'ın kendi diliminde çözülür; UTC takvim ayı kullanılmaz. Faturalanan ay zaten yerel aydır (00016). UTC ayı, yönettiği aydan 14 saate kadar sapardı. Ölçüldü: Malta'da `2026-02-28T23:30Z` Mart'tır, UTC'de Şubat'tır. `TestPlanInForce_AMonthIsGovernedByThePlanAtItsStart` bu farkı ayırt eder.

**Ticari karar; kullanıcı (A)'yı onayladı, 2026-11-09.** Plan bugün **fiyatı** belirlemez; fiyat ayrı sütundur, `price_per_employee_month`. Plan yalnız bir ayın ücretsiz olup olmadığını belirler: founding'de kayıt ayı ve sonraki iki ay ücretsizdir. Her ay tek bir boolean'dır, oranlama (proration) yoktur. Ama ay içindeki bir değişikliğin o ayı mı yoksa bir sonrakini mi yöneteceği, kimin ne ödeyeceğini değiştirir. Bu yüzden ticari bir karardır. Bu ADR'nin ilk taslağı aksini söylüyordu; denetim haklı olarak çürüttü. Ayda tek plan veren okumalar:

| Seçenek | Anlamı | T27 ile ilişkisi |
|---|---|---|
| **(A) ayın başındaki plan** — seçilen, kullanıcı onayı 2026-11-09 | M içindeki bir değişiklik M+1'den geçerli | Başlamış ay korunur. Ana akımın karşılığı: Stripe subscription schedule'ın faz sınırı, Chargebee'nin "end of term" değişimi. |
| (B) ayın sonundaki plan | M içindeki bir değişiklik M'yi de yönetir (`tappa_plan_in_force`'a `period_to` verilir) | T27'yi yeniden **açmaz**: M bittikten sonraki bir değişiklik M'ye dokunamaz. Bedeli, M sürerken önizlemenin ay sonuna kadar oynamasıdır. |
| (B′) kapanış anındaki plan | ay, kapatıldığı andaki planla dondurulur | 00097'den önceki davranış. Bitmiş ama kapatılmamış ay sonraki bir değişimi izler. **T27'nin ta kendisi.** |

"Yükseltme hemen geçerli" istisnasının karşılığı yok: iki plan arasında fiyat farkı yok. founding→standard müşteri için bir kayıptır (ücretsiz ayları gider), standard→founding bir hediyedir. (A) ile ikisi de sonraki aydan başlar. (B) seçilseydi değişen tek şey okuyucunun karşılaştırma anı olurdu (`period_from` yerine `period_to`); şema aynı kalırdı.

## 3. Geri doldurma (backfill)

00097, her tenant için bir satır yazar: mevcut planı, `effective_from = tenants.created_at`, `changed_by = NULL`.

- **Neden `created_at`, neden en erken faturalama ayının başı değil.** Plan, satır yazıldığında belirlendi: `tenants.plan`'ın INSERT anındaki DEFAULT'u. `tappa_app` onu hiç yazamadı (00016). Doğru olan an `created_at`'tir. Ay başı, planı işletme var olmadan öncesine tarihlerdi. Okuyucunun daha erken bir ana ihtiyacı yok: tenant'ın ilk satırından önce başlayan bir ayı o ilk satır yönetir.
- **Sınır, yazılı.** Sahibin daha önce **elle** değiştirdiği bir planın tek izi, dondurulmuş bir ayın `billing_periods.plan` değeridir. Böyle bir tenant için satır mevcut planı kayıttan itibaren tarihler. Bu, yerine geçtiği aritmetikten (her kapatılmamış ayı mevcut planla fiyatlıyordu) asla daha yanlış değildir. Dondurulmuş ay ise kendi satırından okunur. Bir ret (precondition) tartıldı ve bırakıldı. Geliştirme veritabanları tam bu şekli taşıyor: fatura fixture'ları ayları kapattıktan sonra planı yeniden yazıyor (ölçüldü, şerit veritabanında faturalama testlerinin bir koşusundan sonra 13 dondurulmuş ay). Üretimde ise bu bir ölçüm sorusudur. **Deploy'dan önce koşulacak salt-okuma sorgusu:**

  ```sql
  SELECT count(*) FROM billing_periods b JOIN tenants t ON t.id = b.tenant_id WHERE b.plan <> t.plan;
  ```

  Sıfır değilse geri doldurma o tenant'lar için "en iyi bilgi"dir. Doğrusu için 00097'nin yorumundaki yol izlenir: plan geri alınır, migrate edilir, sonra yeniden değiştirilir.
- **Tetiği atlamak yanlış bir geçmiş bırakır.** Sahip ya da süper kullanıcı tetiği atlayabilir: `SET session_replication_role = replica`, `ALTER TABLE tenants DISABLE TRIGGER`, ya da tetikleri kapalı bırakılmış bir geri yükleme. Bu durumda yapılan plan değişikliği hiçbir satır yazmaz. Geçmişi olan bir tenant'ta son satır artık başka bir planı adlandırır ve sonraki her ay eski planla fiyatlanır. Geçmişi olmayan bir tenant'ta iz kalmaz, ama sonraki gerçek değişiklik ilk satırı `OLD`'dan, yani atlanmış plandan yazar. Bu **yanlış bir ilk satırdır**: atlanmış plan kayıttan beri yürürlükteymiş gibi görünür. Değişmez olan ve izlenen kontrol: her tenant için `tappa_plan_in_force(id, clock_timestamp(), plan) = plan`. `scripts/pg-restore-verify.sh` bölüm 7 bunu her geri yüklemede sınar ve bozuksa reddeder (`TestRestoreVerify_PlanHistoryQuery`). Neden orada: geri yükleme, veritabanının servise girmeden önce bütünüyle sahip olarak okunduğu rutin andır. Bölüm 5a'nın kuralı da geçerlidir: kaynağın zaten taşıdığı bir kusur sadakatle kopyalanır ve o da servise girmemelidir. Canlı veritabanı için aynı sorgu elle koşulur:

  ```sql
  SELECT count(*) FROM tenants t WHERE tappa_plan_in_force(t.id, clock_timestamp(), t.plan) IS DISTINCT FROM t.plan;
  ```

  Kontrolün sınırı yazılıdır: geçmişi olmayan bir tenant'taki atlanmış değişiklik burada görünmez (okuyucu `tenants.plan`'a düşer).
- **00097'den sonra doğan tenant'ın satırı yoktur**, ilk plan değişikliğine kadar. Kayıt yolu buraya bir şey yazmaz, bu yüzden `tappa_app`'in tabloda yazma yetkisi yoktur. İlk değişiklikte tetik önce `OLD`'dan ilk satırı yazar (kayıttan beri sahip olduğu plan), sonra değişikliği. Satırı olmayan tenant'ı okuyucu `tenants.plan`'la yönetir, çünkü plan hiç değişmemiştir.

## 4. Yazıcı — tetik, ve "kim"

`tenants` üzerinde `AFTER UPDATE … WHEN (OLD.plan IS DISTINCT FROM NEW.plan)` vardır. `UPDATE OF plan` **değildir** (güvenlik denetimi, DÜŞÜK, 2026-11-09). Sütuna özgü bir tetik yalnız ifadenin SET listesi `plan`'ı adlandırdığında çalışır. Bu durumda başka bir sütunun UPDATE'inde `NEW.plan`'ı değiştiren bir BEFORE tetiği planı geçmişsiz taşırdı. AFTER tetiğinin WHEN'i ise satırı saklandığı haliyle, bütün BEFORE tetiklerinden sonra okur (`TestTenantPlanHistory_ABeforeTriggerThatMovesThePlanStillLeavesItsRow`; mutasyonla ölçüldü). Fonksiyon `tappa_record_plan_change()`'tir, SECURITY INVOKER: satır, planı değiştirenin yetkisiyle yazılır. Bugün bu sahiptir; yarın OP-17'nin tanımlayıcısı olur. `tenant_plan_history`'de INSERT yetkisi olmayan bir rol planı değiştirirse UPDATE'in tamamı 42501 ile düşer. Plan, geçmişi olmadan değişemez. `effective_from` hiçbir zaman çağıranın değildir: duvar saatidir (`clock_timestamp()`). Ürün yolu geriye tarih atamaz.

**Kim:** `changed_by` sütunu `platform_admins(id)`'ye anahtarlıdır (00098). Değeri transaction-yerel `tappa.plan_changed_by` ayarından okunur. Uuid olmayan bir değer 22P02 ile, operatör adlandırmayan bir değer 23503 ile UPDATE'i reddeder (ölçüldü). Bu kimlik doğrulama değil, **atıftır**. Satırı ancak `tenants.plan`'ı yazabilen bir rol üretebilir. NULL, operatör yüzeyinden gelmediği anlamına gelir: geri doldurma, ilk satır ya da elle yazılmış bir UPDATE.

**OP-17'ye kalan (bu ADR inşa etmez):** `op_*` plan yazıcısı. Sahibi `tappa_opdefiner`, SECURITY DEFINER, `SET search_path`, `REVOKE ALL … FROM PUBLIC`, açık `GRANT EXECUTE … TO tappa_operator`. Aynı gövdede sırayla: oturum yüklemi, `set_config('tappa.plan_changed_by', v_admin, true)` (**yalnız transaction-yerel ve UPDATE'ten hemen önce**), `UPDATE public.tenants … WHERE t.id = p_tenant_id`, `operator_audit_log` satırı. **Oturum düzeyinde `SET tappa.plan_changed_by` yasaktır.** Tetik oturum düzeyindeki bir değeri de okur, ve havuzdan dönen bir bağlantıda kalan değer, sonraki elle yapılmış bir değişikliği yanlış operatöre atfederdi (CLAUDE.md §6'nın `SET LOCAL` kuralıyla aynı gerekçe). Tanımlayıcıya `UPDATE (plan) ON tenants` ve `INSERT (tenant_id, plan, effective_from, changed_by) ON tenant_plan_history` verilir. İleri tarihli bir değişiklik ("Ocak'tan itibaren standard") bu şemada yoktur. Gerekirse kendi kararıyla gelir.

## 5. Okuyucu

`tappa_plan_in_force(p_tenant_id, p_at, p_current_plan)` STABLE ve SECURITY INVOKER'dır, `SET search_path` taşır. Cevabı sırayla şöyle bulur:

1. `p_at`'ten önceki ya da ona eşit en son satır;
2. yoksa en eski satır;
3. yoksa `p_current_plan`.

Üç çağıranı vardır: `PreviewBillingPeriod`, `CloseBillingPeriod` (`tappa_app` olarak, RLS açık, `@tenant_id` ile) ve 00098'in `op_read_tenant_billing`'i (`tappa_opdefiner` olarak, RLS'siz). Hepsi ayın `from_at`'ını verir. Gövdenin `h.tenant_id = p_tenant_id` yüklemi `tappa_app` için kemerdir, tanımlayıcı için tek engeldir (ADR 0021 §3). Bağlam dışındaki bir `tappa_app` çağrısı satır görmez ve `p_current_plan`'a düşerdi. Ama iki çağıran da `p_current_plan`'ı aynı ifadede `tenants`'tan okur, ve `tenants` bağlam dışında satır vermez. Düşülecek bir rakam kalmaz.

`op_read_tenant_billing` 00032'nin gövdesidir, yalnız canlı aylarında değişiklik vardır: plan, `first_chargeable` ve `free_period` artık o ayın yürürlükteki planından gelir. Bilet, sayfa sınırı, dondurulmuş aylar ve kemer aynıdır. Önce md5 ön koşulu sınanır (`77ee6860…`, 00032'nin gövdesi, ölçüldü). Sonra CREATE OR REPLACE çalışır; sahip, `REVOKE PUBLIC` ve `GRANT EXECUTE tappa_operator` yeniden yazılır. Okumanın audit satırı `op_begin_read`'indir ve bu dosya ona dokunmaz.

## 6. Yetkiler

| Rol | `tenant_plan_history` | `tappa_plan_in_force` | `tappa_record_plan_change` |
|---|---|---|---|
| `tappa_app` | SELECT (`tenant_id`, `plan`, `effective_from`); INSERT/UPDATE/DELETE/TRUNCATE yok | EXECUTE | yok |
| `tappa_opdefiner` (00098) | aynı üç sütun SELECT; yazma yok | EXECUTE | yok |
| `tappa_operator` ve öteki roller | hiçbir şey | yok | yok |
| `tappa_owner` | sahip; UPDATE/DELETE/TRUNCATE `tappa_forbid_mutation`'a çarpar | sahip | sahip |

`changed_by`, `id` ve `recorded_at` hiçbir role açılmaz. Hangi operatörün değiştirdiği platformun kaydıdır, tenant'ın değil. Katalog testleri: `privcatalog_test.go`, `m11schema_test.go` matrisi, `operatorschema_test.go` (`opdefinerTenantGrants`), `operatorplaques_test.go` (`opDefinerForeignExecAllowed`, altıncı). `scripts/pg-restore-verify.sh`'in `trunc_tables` listesi on iki tabloya çıktı (bölüm 5 ve 5a), `scripts/redline-check.sh`'in APPEND_ONLY listesi de. `test/fixtures/restore-chain.sql` tabloya ürünün yoluyla bir satır yazar.

## 7. Geri alma

- **00098 Down:** okumayı 00032'nin gövdesine harfi harfine geri koyar (md5 geri gelir, ölçüldü). Tanımlayıcının üç sütununu ve EXECUTE'unu geri alır, anahtarı düşürür. Hiçbir koşulda reddetmez, çünkü satır silmez.
- **00097 Down:** bir değişiklik kaydedildiyse (iki ya da daha fazla satırlı bir tenant) 55000 ile reddeder ve hiçbir şeyi değiştirmez. Yoksa tetiği, iki fonksiyonu ve tabloyu düşürür. Tek satırlı tenant'ın satırı `tenants.plan`'ın zaten söylediğidir. Önce tablolar kilitlenir, sonra bakılır.
- **Ölçüldü (`planhist_rt2`):** tenant'sız down-to 96 → up; seed sonrası down → up (2 satır, ikisi de plan = `tenants.plan` ve `effective_from = created_at`); bir plan değişikliğinden sonra down → 00098 iner, 00097 reddeder (sürüm 97'de kalır) → up.
- **Test yürüyüşleri:** operatörün `opAtVersion`'ı 00097'yi atlar (operatör nesnesi yok; 00098 yürünür). Kimlik yürüyüşleri ise 00097'yi yürür. Faturalama testlerinin commit ettiği değişiklikleri `aiLiftLaterDownRefusals` işlemin içinde kaldırır. Bu, 00063'ün askı kaldırmasının kalıbıdır ve rollback ile geri gelir.

## 8. Kalanlar

- **T37** (zone düzenlemesi) açık kalır. Bir zone değişikliği `from_at`'ı 14 saate kadar kaydırır. Sınıra o kadar yakın bir plan değişikliğinin hangi ayı yöneteceği zone'a bağlıdır. OP-17 hâlâ T37'ye de bağlıdır.
- **T268:** fiyat (`price_per_employee_month`) aynı sınıftadır. Bitmiş ama kapatılmamış bir ay, sonraki bir fiyat değişikliğini izler. Bu karar fiyatı kapsamaz.
- **T269:** `FirstChargeableMonthShift` (T37'nin zone kapısı) hâlâ `tenants.plan`'ı okur, yürürlükteki planı değil. Planı değişmiş bir tenant'ta kapı yanlış ayı karşılaştırabilir.
