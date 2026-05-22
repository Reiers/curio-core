//go:build cgo

package webrpc

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/lotus/chain/types"
)

type OpenDealInfo struct {
	Actor        int64     `db:"sp_id"`
	SectorNumber uint64    `db:"sector_number"`
	PieceCID     string    `db:"piece_cid"`
	PieceSize    uint64    `db:"piece_size"`
	RawSize      uint64    `db:"data_raw_size"`
	CreatedAt    time.Time `db:"created_at"`
	SnapDeals    bool      `db:"is_snap"`

	PieceSizeStr string `db:"-"`
	CreatedAtStr string `db:"-"`
	PieceCidV2   string `db:"-"`

	Miner string
}

func (a *WebRPC) DealsPending(ctx context.Context) ([]OpenDealInfo, error) {
	deals := []OpenDealInfo{}
	err := a.deps.DB.Select(ctx, &deals, `SELECT sp_id, sector_number, piece_cid, piece_size, data_raw_size, created_at, is_snap FROM open_sector_pieces ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}

	for i, deal := range deals {
		deals[i].PieceSizeStr = types.SizeStr(types.NewInt(deal.PieceSize))
		deals[i].CreatedAtStr = deal.CreatedAt.Format("2006-01-02 15:04:05")
		maddr, err := address.NewIDAddress(uint64(deals[i].Actor))
		if err != nil {
			return nil, err
		}
		deals[i].Miner = maddr.String()
		pcid, err := cid.Parse(deals[i].PieceCID)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse piece cid: %w", err)
		}
		pcid2, err := commcid.PieceCidV2FromV1(pcid, deals[i].RawSize)
		if err != nil {
			return nil, xerrors.Errorf("failed to get commp: %w", err)
		}
		deals[i].PieceCidV2 = pcid2.String()
	}

	return deals, nil
}

// TODO(curio-core): DealsSealNow stubbed out — it called market/storageingest.SealNow
// which pulls in tasks/seal (sealing pipeline). PDP-only Curio doesn't seal sectors;
// MK12 page is still under review with Andy, so the RPC name is preserved but the
// implementation is disabled. Re-wire only if MK12 stays AND a non-sealing path exists.
func (a *WebRPC) DealsSealNow(ctx context.Context, spId, sectorNumber uint64) error {
	_ = spId
	_ = sectorNumber
	return xerrors.New("DealsSealNow disabled in curio-core (sealing pipeline removed)")
}
