package internal

const Server = "MOPSRV01\\SQL1"
const AdvoPro = "AdvoPro"

const StatusFemQuery = `
SELECT
	f.Sagsnr as sagsnr,
	f.Status as status,
	f.ForlobInfo as forlobInfo,
	f.Fristdato,
	d.Navn as navn,
	d.Adresse as adresse,
	d.Postnr as postnr,
	d.Bynavn as bynavn,
	d.Noter as noter,
	d.DebitorId as debitorId
FROM
	vwInkassoForlob f
JOIN
	vwInkassoForlobDebitor fd ON fd.ForlobId = f.ForlobId
JOIN
	vwInkassoDebitor d ON d.DebitorId = fd.DebitorId
WHERE
	f.Status in (5)
`

const SagsnrQuery = `
SELECT 
    F.Sagsnr as sagsnr,
    D.Navn as navn,
    D.Adresse as adresse
FROM
    vwInkassoForlob F
JOIN
    vwInkassoForlobDebitor FD on FD.ForlobId = F.ForlobId
JOIN
    vwInkassoDebitor D ON D.DebitorId = FD.DebitorId
WHERE
    F.Sagsnr = @p1
`
const debitorQuery = `
SELECT
	*
from
	vwInkassoDebitor d
where
	d.DebitorId = @p1
`

const debtInfo = `DECLARE @sagsnr INT = 511184;

WITH FilteredIndbetalinger AS (
    SELECT
        a.PostId,
        a.klientnr,
        a.Sagsnr,
        a.FordringId,
        a.Omkostninger,
        a.Renter,
        a.Hovedstol,
        -- Include ForlobId for filtering but not deduplication
        a.ForlobId,
		ISNULL(fundament.FundamentId,0) as FundamentId
    FROM
        mopIndbetalingAfskrevet a
        FULL JOIN vwInkassoFundamentFilter fundament ON fundament.ForlobId = a.ForlobId
    WHERE
        a.MasterId = (SELECT MAX(MasterId) FROM mopIndbetalingAfskrevet)
        AND a.Bilagsdato > ISNULL(fundament.Dato, 0)
        AND ISNULL(fundament.RowNumber, 1) = 1
		and Sagsnr = @sagsnr
)
, Deduplicated AS (
    SELECT
        PostId AS PostId,
        klientnr,
        Sagsnr,
        FordringId,
		ForlobId,
        Omkostninger AS Omkostninger,
        Renter AS Renter,
        Hovedstol AS Hovedstol,
		FundamentId
    FROM
        FilteredIndbetalinger
    GROUP BY
        klientnr, Sagsnr, FordringId, Omkostninger,Renter,Hovedstol,FundamentId,PostId, ForlobId
), Indbetaling as (
SELECT
    klientnr,
    Sagsnr,
    FordringId,
	ForlobId,
	FundamentId,
    SUM(Omkostninger + Renter + Hovedstol) AS totalIndbetalt,
    SUM(Omkostninger) AS totalOmk,
    SUM(Renter) AS totalRenter,
    SUM(Hovedstol) AS totalHoved
FROM
    Deduplicated
GROUP BY
    klientnr, Sagsnr, FordringId, FundamentId,ForlobId)


select
	ISNULL(i.totalIndbetalt,0) as 'SumIndbetalinger',
	ISNULL(i.totalIndbetalt + brev.Restgeld,0) as 'restgeldAntaget',
	b.RestanceDato,
	b.KreditorHovedstol,
	brev.Restgeld as 'restgeldVedBrev',
	brev.Indbetalt as 'SumIndbetalingVedBrev'
from
	InkassoFordringBeregnPr(GETDATE()) b
left JOIN
	Indbetaling i on  b.ForlobId = i.ForlobId 
left join
	apFlet.vw110_Brev010_ny1 brev ON b.Sagsnr = brev.Sagsnr 
where
	b.Sagsnr = @sagsnr
`
