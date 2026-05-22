-- Hand-written override for Curio Core (not a mechanical port).
-- Source reference: github.com/filecoin-project/curio harmony/harmonydb/sql/20240212-common-layers.sql
-- See ../../../docs/SQL-CLASSIFICATION.md and DAY-3-NOTES.md for reasoning.
--

-- Replacement for Postgres seed of harmony_config layer presets.
-- The original file inserted rows for: post, gui, seal, seal-gpu,
-- seal-snark, sdr, storage. Of those, only 'gui' is plausibly useful
-- in a PDP-only Curio Core build; everything else seeds sealing-pipeline
-- subsystems we don't ship. So we seed just 'gui' here, plus a
-- 'pdp' layer that turns on the PDP-shaped subsystems Curio Core boots.
-- TOML config bodies are stored as TEXT verbatim; Curio's deps/config
-- toml decoder handles the boolean keywords inside the body.
INSERT INTO harmony_config (title, config) VALUES
  ('gui', '[Subsystems]
EnableWebGui = true
'),
  ('pdp', '[Subsystems]
EnablePDP = true
EnableParkPiece = true
EnableSendChainMsg = true
')
ON CONFLICT (title) DO NOTHING;
