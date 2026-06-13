import React, { useState, useEffect, useCallback, useRef } from 'react';
import { drafts } from '@/services/api';
import { gui } from '@/types/models';
import { trackEvent } from '@/services/analytics';
import ColorIdentity from './ColorIdentity';
import './CurrentPackPicker.css';

interface CurrentPackPickerProps {
    sessionID: string;
    onRefresh?: () => void;
}

const CARD_BACK_URL = '/back.png';

const CurrentPackPicker: React.FC<CurrentPackPickerProps> = ({ sessionID, onRefresh }) => {
    const [packData, setPackData] = useState<gui.CurrentPackResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    const loadPackData = useCallback(async () => {
        try {
            setLoading(true);
            setError(null);
            const data = await drafts.getCurrentPackWithRecommendation(sessionID);
            setPackData(data);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load pack data');
            console.error('Error loading pack data:', err);
        } finally {
            setLoading(false);
        }
    }, [sessionID]);

    useEffect(() => {
        if (sessionID) {
            loadPackData();
        }
    }, [sessionID, loadPackData]);

    // Analytics: feature_ml_suggestions_viewed — fires once per unique pack
    // state (session + pack + pick) when the recommendation surface is shown.
    // No PII: suggestion_count is a count; context is a literal enum value.
    const lastViewedKeyRef = useRef<string | null>(null);
    useEffect(() => {
        if (!packData || !packData.recommended_card || !packData.cards?.length) return;
        const key = `${sessionID}/${packData.pack_number}/${packData.pick_number}`;
        if (lastViewedKeyRef.current === key) return;
        lastViewedKeyRef.current = key;
        trackEvent({
            name: 'feature_ml_suggestions_viewed',
            properties: {
                suggestion_count: packData.cards.length,
                context: 'draft',
            },
        });
    }, [sessionID, packData]);

    const handleRefresh = () => {
        loadPackData();
        if (onRefresh) {
            onRefresh();
        }
    };

    const getTierClass = (tier: string): string => {
        const t = tier.toLowerCase();
        if (['a', 'b', 'c', 'd', 'f'].includes(t)) {
            return `tier-badge--${t}`;
        }
        // S-tier and unknown tiers fall back to the --s modifier (gold, not in §7.3 A/B/C/D/F).
        return 'tier-badge--s';
    };

    const renderColorIndicator = (colors: string[] | undefined) => {
        return <ColorIdentity colors={colors} size="sm" />;
    };

    if (loading) {
        return (
            <div className="current-pack-loading">
                <div className="loading-spinner"></div>
                <p>Loading current pack...</p>
            </div>
        );
    }

    if (error) {
        return (
            <div className="current-pack-error">
                <p>{error}</p>
                <button onClick={handleRefresh} className="retry-btn">Retry</button>
            </div>
        );
    }

    if (!packData || !packData.cards || packData.cards.length === 0) {
        return (
            <div className="current-pack-empty">
                <p>No pack data available</p>
                <p className="help-text">Pack data will appear when you start a draft pick</p>
            </div>
        );
    }

    return (
        <div className="current-pack-container">
            <div className="current-pack-header">
                <h2>{packData.pack_label}</h2>
                <div className="pack-info">
                    <span className="pool-info">Pool: {packData.pool_size} cards</span>
                    {packData.pool_colors && packData.pool_colors.length > 0 && (
                        <span className="pool-colors">
                            Colors: {renderColorIndicator(packData.pool_colors)}
                        </span>
                    )}
                    <button onClick={handleRefresh} className="refresh-btn" title="Refresh pack data">
                        Refresh
                    </button>
                </div>
            </div>

            {/* Recommended Pick Banner */}
            {packData.recommended_card && (
                <div className="recommended-banner" data-testid="recommended-banner">
                    <span className="rec-label">Recommended Pick:</span>
                    <span className="rec-card-name">{packData.recommended_card.name}</span>
                    {packData.recommended_card.tier && (
                        <span className={`rec-tier rec-tier--${packData.recommended_card.tier.toLowerCase()}`}>
                            {packData.recommended_card.tier}
                        </span>
                    )}
                    {packData.recommended_card.reasoning && (
                        <span className="rec-reason" data-testid="rec-reasoning">{packData.recommended_card.reasoning}</span>
                    )}
                    {packData.recommended_card.low_confidence === true && (
                        <span className="rec-low-confidence" data-testid="rec-low-confidence">
                            Limited data — early format
                        </span>
                    )}
                    {packData.recommended_card.gihwr == null && (
                        <span className="rec-no-arena-data" data-testid="rec-no-arena-data">
                            Community consensus / No Arena data yet
                        </span>
                    )}
                </div>
            )}

            {/* Pack Cards Grid */}
            <div className="pack-cards-grid" data-testid="pack-cards-grid">
                {packData.cards.map((card, index) => (
                    <div
                        key={card.arena_id || index}
                        className={`pack-card ${card.is_recommended ? 'recommended' : ''}`}
                        data-testid={`pack-card-${card.arena_id || index}`}
                    >
                        <div className="card-image-container">
                            <img
                                src={card.image_url || CARD_BACK_URL}
                                alt={card.name}
                                className="card-image"
                                loading="lazy"
                                onError={(e) => {
                                    (e.target as HTMLImageElement).src = CARD_BACK_URL;
                                }}
                            />
                            {card.tier && (
                                <div
                                    className={`tier-badge ${getTierClass(card.tier)}`}
                                    data-testid={`tier-badge-${card.arena_id || index}`}
                                    aria-label={`Tier ${card.tier}`}
                                >
                                    {card.tier}
                                </div>
                            )}
                            {card.is_recommended && (
                                <div className="recommended-indicator" data-testid="best-pick-indicator">Best Pick</div>
                            )}
                            {card.low_confidence && (
                                <div
                                    className="low-confidence-pill"
                                    data-testid={`low-confidence-${card.arena_id || index}`}
                                    title="Fewer than 500 games in hand — treat this rating as a rough guide"
                                >
                                    Limited data
                                </div>
                            )}
                        </div>
                        <div className="card-info">
                            <div className="card-name">{card.name}</div>
                            <div className="card-stats">
                                {renderColorIndicator(card.colors)}
                                <span className="gihwr" title="Games In Hand Win Rate">
                                    {card.gihwr ? `${(card.gihwr * 100).toFixed(1)}%` : '—'}
                                </span>
                                <span className="alsa" title="Average Last Seen At">
                                    ALSA: {card.alsa?.toFixed(1)}
                                </span>
                            </div>
                            {card.reasoning && (
                                <div className="card-reasoning">{card.reasoning}</div>
                            )}
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
};

export default CurrentPackPicker;
