// @ts-check

/** @typedef {{ id: string, label: string, count: number, lastWonDate: string | null }} RankedStatRow */

/**
 * @param {{
 *   title: string,
 *   subtitle?: string,
 *   emptyMessage: string,
 *   items: RankedStatRow[],
 * }} props
 */
function StatRankingList({ title, subtitle, emptyMessage, items }) {
    return (
        <section className="card shadow-sm h-100">
            <div className="card-body">
                <div className="mb-3">
                    <h2 className="h5 mb-1">{title}</h2>
                    {subtitle && <p className="text-body-secondary mb-0">{subtitle}</p>}
                </div>

                {items.length === 0 ? (
                    <p className="text-body-secondary mb-0">{emptyMessage}</p>
                ) : (
                    <ol className="list-group list-group-numbered mb-0">
                        {items.map((item) => (
                            <li key={item.id} className="list-group-item d-flex justify-content-between align-items-start gap-3">
                                <div className="ms-2 me-auto">
                                    <div className="fw-semibold">{item.label}</div>
                                    <div className="small text-body-secondary">{item.id}</div>
                                </div>
                                <div className="text-end">
                                    <div className="fw-semibold">{item.count}</div>
                                    {item.lastWonDate && <div className="small text-body-secondary">Last win {item.lastWonDate}</div>}
                                </div>
                            </li>
                        ))}
                    </ol>
                )}
            </div>
        </section>
    );
}

export default StatRankingList;
