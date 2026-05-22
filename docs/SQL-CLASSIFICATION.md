# SQL Migration Classification

Source: `~/.openclaw/workspace/projects/curio-fork/harmony/harmonydb/sql/` at integ/task SHA `21531097`.
118 migration files total.

## Categories

- **KEEP-INFRA** — Harmonytask / config / machine / message-send / piece-park / eth-chain plumbing. PDP and the task scheduler need it. Port to SQLite.
- **KEEP-PDP** — PDP-specific schema (pdp_*, pdpv0 tables) AND the tables PDP transitively queries (mk20 deal status, IPNI for pdpv0, market piece deal index, sector_location/storage_path used as a generic file index with `miner_id = 0`). Port to SQLite.
- **DROP-SEALING** — SDR / snap / wdpost / winning / window / mining / sector_meta / unseal / scrub / commit-batching. Sealing pipeline. Not in Curio Core.
- **DROP-CLUSTER** — Proofshare (cross-node proof sharing). Curio Core is single-node. Also balancemgr (depends on cross-task plumbing we don't ship) and wallet-exporter (multi-wallet bookkeeping for cluster ops).
- **DROP-LEGACY** — Test scratch + dead migrations + mk12/boost-only market plumbing not referenced by PDP.

## Decision rationale

- The PDP go code (`tasks/pdp/`, `tasks/pdpv0/`) was grep'd for every table name it reads or writes. Result:
  `harmony_task, harmony_config, harmony_machines, parked_pieces, parked_piece_refs,
   eth_keys, message_waits_eth, message_sends_eth,
   pdp_* (all),
   ipni, ipni_head, ipni_peerid,
   market_mk20_deal, market_piece_deal,
   sector_location, storage_path`.
- Any migration that creates, alters, indexes, or maintains one of those tables → KEEP-{INFRA,PDP}.
- Any migration that only touches sealing-pipeline tables (`sectors_sdr_pipeline`, `sectors_meta`, `sectors_snap_pipeline`, `mining_tasks`, `wdpost_*`, `scrub_*`, `f3_tasks`, `proofshare_*`, `balance_manager_*`, `sectors_cc_scheduler`, etc.) → DROP-{SEALING,CLUSTER,LEGACY}.
- Mk12/mk20 carries weight here: PDP writes to `market_mk20_deal`, so we have to keep the chain that creates that table (which sits inside `20240731-market-migration.sql` → `20250505-market-mk20.sql`). The mk12-only filter / fix files are dropped since PDP doesn't touch them.

## Table

| Filename | Category | Reason |
|---|---|---|
| 20230706-itest_scratch.sql | DROP-LEGACY | Test scratch table. Not used by PDP or harmonytask. |
| 20230712-sector_index.sql | KEEP-INFRA | Creates `sector_location` + `storage_path`. PDP task_commp joins these (`miner_id = 0` for the PDP virtual miner). |
| 20230719-harmony.sql | KEEP-INFRA | Core harmonytask schema (harmony_machines, harmony_task, harmony_task_history, harmony_task_follow, harmony_task_impl). Mandatory. |
| 20230823-wdpost.sql | DROP-SEALING | WindowPoSt partition tasks. Sealing. |
| 20230919-config.sql | KEEP-INFRA | Creates `harmony_config`. Required by deps/config + layered config. |
| 20231103-chain_sends.sql | KEEP-INFRA | Creates `message_sends`, `message_send_locks`. Curio's filecoin-side send queue. PDP uses the eth_* variants but harmonytask also uses message_sends for chain messages. Keep for completeness. |
| 20231110-mining_tasks.sql | DROP-SEALING | Block mining (`winning`). |
| 20231113-harmony_taskhistory_oops.sql | KEEP-INFRA | Adds `completed_by_host_and_port` to harmony_task_history. Required for harmonytask scheduling/heuristics. |
| 20231120-testing1.sql | DROP-LEGACY | `harmony_test` testing scratch table. |
| 20231217-sdr-pipeline.sql | DROP-SEALING | The sealing pipeline (sectors_sdr_pipeline + friends). |
| 20231225-message-waits.sql | KEEP-INFRA | Creates `message_waits`. Harmonytask + PDP indirectly (eth variant lives in 20240929 but the FIL variant is referenced by message-send-task plumbing we inherit). |
| 20240212-common-layers.sql | KEEP-INFRA | Seeds `harmony_config` with common layer presets. PDP reads layer config; the layers themselves are mostly sealing but the row scaffolding is needed. We will trim the sealing entries in this file during translation. |
| 20240228-piece-park.sql | KEEP-INFRA | Creates `parked_pieces` + `parked_piece_refs`. PDP's primary storage layer. |
| 20240317-web-summary-index.sql | KEEP-INFRA | Index on `harmony_task_history`. Used by harmonytask UI / metrics. Cheap to keep. |
| 20240401-storage-miner-filter.sql | KEEP-INFRA | Adds `allow_miners`/`deny_miners` columns on `storage_path`. PDP joins storage_path. |
| 20240402-sdr-pipeline-ddo-deal-info.sql | DROP-SEALING | Altering `sectors_sdr_initial_pieces`. |
| 20240404-machine_detail.sql | KEEP-INFRA | Creates `harmony_machine_details`. Harmonytask. |
| 20240416-harmony_singleton_task.sql | KEEP-INFRA | Creates `harmony_task_singletons`. Harmonytask. |
| 20240417-sector_index_gc.sql | KEEP-INFRA | Creates `sector_path_url_liveness`. References `storage_path`. The PDP storage backend reads URL liveness. |
| 20240420-web-task-indexes.sql | KEEP-INFRA | Replaces 20240317's index with a better one for harmonytask UI. |
| 20240425-sector_meta.sql | DROP-SEALING | `sectors_meta` is the sealing-side sector catalog. |
| 20240501-harmony-indexes.sql | KEEP-INFRA | Indexes on harmony_task_history. |
| 20240507-sdr-pipeline-fk-drop.sql | DROP-SEALING | FK cleanup on sectors_sdr_pipeline. |
| 20240508-open-deal-sectors.sql | DROP-SEALING | `open_sector_pieces` + sectors_sdr_initial_pieces. Sealing-side market plumbing. |
| 20240522-ts-to-timestampz.sql | KEEP-INFRA | Converts harmony_machines/harmony_task timestamps to TIMESTAMPTZ. **Cross-cuts**: also touches sealing tables. We'll port only the harmony_*/parked_*/storage_path/sector_location subset. |
| 20240527-machine_name.sql | KEEP-INFRA | Adds `machine_name` to harmony_machine_details. |
| 20240529-sdr-pipeline-task-extract.sql | DROP-SEALING | PG function on sdr-pipeline. |
| 20240606-storage-gc.sql | DROP-SEALING | `storage_removal_marks` is sealing-sector-file GC. PDP uses parked_pieces cleanup instead. |
| 20240610-mining-inclusion-checks.sql | DROP-SEALING | `mining_tasks.included`. |
| 20240611-snap-pipeline.sql | DROP-SEALING | `sectors_snap_pipeline`. |
| 20240612-deal-proposal.sql | DROP-SEALING | `sectors_meta_pieces`. |
| 20240617-synthetic-proofs.sql | DROP-SEALING | sectors_sdr_pipeline `task_id_synth`. |
| 20240701-batch-sector-refs.sql | DROP-SEALING | `batch_sector_refs`. |
| 20240730-alerts.sql | KEEP-INFRA | `alerts` table. Harmonytask alerting. Cheap to keep, used by general infra. |
| 20240731-market-migration.sql | KEEP-PDP | Creates `market_mk12_deals`, `market_piece_deal`, `market_piece_metadata`, `market_direct_deals` etc. PDP queries `market_piece_deal` and the mk20 alteration chain depends on this. |
| 20240802-sdr-pipeline-user-expiration.sql | DROP-SEALING | sectors_sdr_pipeline. |
| 20240809-snap-failures.sql | DROP-SEALING | sectors_snap_pipeline. |
| 20240814-pipeline-task-events.sql | DROP-SEALING | `sectors_pipeline_events`. |
| 20240823-ipni.sql | KEEP-PDP | Creates `ipni`, `ipni_head`, `ipni_peerid`. pdpv0 advertises pieces via IPNI. |
| 20240824-longterm-indexes.sql | KEEP-INFRA | Indexes on `message_waits` (PDP-relevant) and `message_sends` (infra). The `mining_base_block` index is dropped from the port. |
| 20240826-sector-partition.sql | DROP-SEALING | sectors_meta. |
| 20240903-unseal-pipeline.sql | DROP-SEALING | sectors_unseal_pipeline. |
| 20240904-scrub-unseal-check.sql | DROP-SEALING | `scrub_unseal_commd_check`. |
| 20240906-http-server.sql | KEEP-INFRA | `autocert_cache`. PDP HTTP server uses Let's Encrypt autocert. |
| 20240927-task-retrywait.sql | KEEP-INFRA | Adds `retries` to harmony_task. Harmonytask. |
| 20240929-chain-sends-eth.sql | KEEP-INFRA | Creates `eth_keys`, `message_sends_eth`, `message_waits_eth`, `message_send_eth_locks`. PDP signs and sends ETH transactions through this layer. |
| 20240930-pdp.sql | KEEP-PDP | THE pdp.v1 schema. pdp_services, pdp_piece_uploads, pdp_piecerefs, pdp_piece_mh_to_commp, pdp_proof_sets, pdp_prove_tasks, pdp_proofset_creates, pdp_proofset_roots, pdp_proofset_root_adds + 5 triggers. |
| 20241017-market-mig-indexing.sql | DROP-LEGACY | `market_mk12_deal_pipeline_migration` — one-time migration for mk12 indexing. PDP doesn't touch this. |
| 20241021-f3.sql | DROP-SEALING | `f3_tasks`. F3 participation. Lantern handles F3 read side; we don't vote. |
| 20241028-padding.sql | DROP-SEALING | PG function `transfer_and_delete_sorted_open_piece` on open_sector_pieces. |
| 20241029-mk12-filters.sql | DROP-LEGACY | mk12 pricing filters. PDP doesn't use mk12. |
| 20241030-deal-label.sql | DROP-LEGACY | mk12 deal label. |
| 20241104-piece-info.sql | KEEP-PDP | `piece_summary` single-row counter table + triggers. Maintained from parked_pieces and PDP touches parked_pieces. Triggers will need translation. |
| 20241105-walletnames.sql | KEEP-INFRA | `wallet_names`. Harmlessly useful for the eth/fil wallet layer. |
| 20241106-market-fixes.sql | KEEP-PDP | Adds `ipni_peerid_sp_id_unique` (used by pdpv0). Also adds a unique index on `sectors_pipeline_events` (sealing); we'll drop just that line during translation. |
| 20241210-sdr-batching.sql | DROP-SEALING | sectors_sdr_pipeline. |
| 20250111-machine-maintenance.sql | KEEP-INFRA | `harmony_machines.unschedulable`. Harmonytask. |
| 20250113-pdp-never-delete.sql | KEEP-PDP | `pdp_proofset_root_adds.roots_added`. PDP v1. |
| 20250115-proofshare.sql | DROP-CLUSTER | proofshare_queue. Cross-node proof sharing. Single-node Curio Core doesn't proof-share. |
| 20250129-msgwait-idx.sql | KEEP-INFRA | Partial index on `message_waits`. |
| 20250220-mk12-ddo.sql | DROP-LEGACY | mk12 DDO. PDP doesn't use mk12. |
| 20250310-fix-ingest.sql | DROP-SEALING | Reworks `transfer_and_delete_sorted_open_piece` (open_sector_pieces). |
| 20250312-batching-functions.sql | DROP-SEALING | sectors_sdr_pipeline batching functions. |
| 20250331-fix-bulk-restart-func.sql | DROP-SEALING | `unset_task_id` PG function on sealing tables. |
| 20250422-msg-wait-timestamp.sql | KEEP-INFRA | `message_waits.created_at`. Required by harmonytask cleanup. |
| 20250423-remove-fee-aggregation.sql | DROP-SEALING | sealing commit fee aggregation. |
| 20250505-market-mk20.sql | KEEP-PDP | The mk20 deal protocol. Creates `market_mk20_deal` and `market_mk20_pipeline` (PDP joins these). Heavy file (1026 lines), much of it altering mk12 tables; we keep the mk20 + market_piece_deal pieces and drop the sealing-pipeline ones. |
| 20250603-pdp-public-service.sql | KEEP-PDP | Seeds default "public" PDP service in pdp_services. |
| 20250619-proofshare-fixes.sql | DROP-CLUSTER | proofshare cleanup. |
| 20250620-proofshare-pow.sql | DROP-CLUSTER | proofshare. |
| 20250724-proofshare-autosettle.sql | DROP-CLUSTER | proofshare. |
| 20250727-balancemgr.sql | DROP-CLUSTER | `balance_manager_*` cross-task. |
| 20250728-proofshare-payment-stats.sql | DROP-CLUSTER | proofshare payments. |
| 20250730-pdp-v0-rename.sql | KEEP-PDP | The big PDP terminology rename: proofset→data_set, root→piece etc. Mandatory for pdpv0. Includes Yugabyte-specific guards we strip. |
| 20250801-proofshare-pipeline.sql | DROP-CLUSTER | proofshare pipeline. |
| 20250803-wallet-exporter.sql | DROP-CLUSTER | wallet_exporter_processing. Multi-wallet exporter. |
| 20250808-cc-scheduler.sql | DROP-SEALING | `sectors_cc_scheduler` for CC sealing. |
| 20250811-fix-commit-batching.sql | DROP-SEALING | sealing commit batching. |
| 20250817-balancemgr-pshare.sql | DROP-CLUSTER | balancemgr + proofshare. |
| 20250818-restart-request.sql | KEEP-INFRA | `harmony_machines.restart_request`. Harmonytask. |
| 20250926-harmony_config_timestamp.sql | KEEP-INFRA | `harmony_config.timestamp`. |
| 20250930-pdp-v0-streaming-upload.sql | KEEP-PDP | `pdp_piece_streaming_uploads`. PDP v0 upload pipeline. |
| 20251004-pdp-v0-indexing.sql | KEEP-PDP | PDP v0 indexing fields. |
| 20251010-pdp-v0-fix-add-piece-constraints.sql | KEEP-PDP | PDP v0 constraint fix. Has Yugabyte-specific guard we strip. |
| 20251011-pdp-v0-ipni-fetch-tracking.sql | KEEP-PDP | `ipni_ad_fetches`. PDP v0 IPNI ad fetch tracking. |
| 20251014-park-piece-optimisation.sql | KEEP-INFRA | Indexes on parked_pieces/parked_piece_refs. |
| 20251015-pdp-v0-piece-adds-datasetid-nullable.sql | KEEP-PDP | PDP v0. |
| 20251027-pdp-v0-filecoin-pay.sql | KEEP-PDP | `filecoin_payment_transactions`. PDP v0 filecoin payment leg. |
| 20251029-pdp-v0-pieceref-cascade.sql | KEEP-PDP | PDP v0 FK cascade fix. |
| 20251125-sector-ext-mgr.sql | DROP-SEALING | `sectors_meta_updates`. |
| 20251231-fix-raw-size.sql | KEEP-PDP | `market_fix_raw_size` task table — referenced by mk20 raw_size backfill which mk20 pipeline depends on. Kept conservatively (small + low risk). |
| 20260109-pdp-v0-pull.sql | KEEP-PDP | `pdp_piece_pulls`. PDP v0 SP-to-SP transfer. |
| 20260110-pdp-v0-termination-handling.sql | KEEP-PDP | PDP v0 termination. |
| 20260112-pdp-v0-efficiency-indexes.sql | KEEP-PDP | Indexes for pdp_piece_uploads / parked_pieces / mk20. |
| 20260116-scrub-commr-check.sql | DROP-SEALING | scrub_commr_check. |
| 20260117-alert-mutes.sql | KEEP-INFRA | alert_mutes table. |
| 20260118-alert-history.sql | KEEP-INFRA | alert_history + alert_comments. |
| 20260122-pdp-v0-deletion-allowed.sql | KEEP-PDP | PDP v0 dataset deletion gate. |
| 20260123-pdp-v0-rename-terminated-at-epoch.sql | KEEP-PDP | PDP v0 column rename. |
| 20260125-fix-process_piece_deal.sql | KEEP-PDP | Fixes `process_piece_deal` PG function. `market_piece_deal` is PDP-touched. |
| 20260201-fix-batching-timeout.sql | DROP-SEALING | sealing batch timeout fixes. |
| 20260203-pdp-v0-delete-task-nullable.sql | KEEP-PDP | PDP v0 nullable fix. |
| 20260211-fix-raw-size-table.sql | KEEP-PDP | Re-keys market_fix_raw_size (continuation of 20251231). |
| 20260215-config-history.sql | KEEP-INFRA | `harmony_config_history`. |
| 20260216-pdp-v0-save-cache.sql | KEEP-PDP | PDP v0 cache flag. |
| 20260222-fix-trigger-timestamps.sql | DROP-SEALING | Fixes set_precommit_ready_at / set_commit_ready_at / set_update_ready_at triggers on sealing tables. |
| 20260314-singleton-run-now.sql | KEEP-INFRA | `harmony_task_singletons.run_now_request`. |
| 20260315-fix-snap-delete-trigger.sql | DROP-SEALING | snap pipeline trigger fix. |
| 20260321-proofshare-commr-idempotency.sql | DROP-CLUSTER | proofshare. |
| 20260407-has-sector-key.sql | DROP-SEALING | sectors_meta column. |
| 20260410-ipni-head-cas.sql | KEEP-PDP | `insert_ad_and_update_head_checked` PG function. Used by IPNI advertising flow that pdpv0 invokes. **Non-trivial PG function translation.** |
| 20260411-sector-live-faulty.sql | DROP-SEALING | sectors_meta. |
| 20260414-pdp-v0-fix-add-piece-constraints.sql | KEEP-PDP | PDP v0 constraint fix continuation. Yugabyte-specific guard stripped. |
| 20260416-mk20-ddo-contracts.sql | KEEP-PDP | `ddo_contracts`. PDP/mk20 allowed-contracts allowlist. |
| 20260424-seal-poller-indexes.sql | DROP-SEALING | sectors_sdr_pipeline indexes. |
| 20260430-harmony-task-history-idx.sql | KEEP-INFRA | harmony_task_history index. |
| 20260501-machine-detail-version.sql | KEEP-INFRA | `harmony_machine_details.version`. |
| 20260511-pdpv0-ipni-tracking.sql | KEEP-PDP | PDP v0 IPNI tracking column addition. |

## Counts

- **KEEP-INFRA**: 35
- **KEEP-PDP**: 30
- **DROP-SEALING**: 37
- **DROP-CLUSTER**: 10
- **DROP-LEGACY**: 6
- **Total**: 118 ✅ (matches `ls *.sql | wc -l` in the source dir)

Files that get ported to `internal/harmonysqlite/migrations/`: 65 (KEEP-INFRA + KEEP-PDP).
Files that get dropped: 53.
