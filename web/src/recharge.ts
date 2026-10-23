import type { RechargePromotion } from './types';

export function rechargePercent(amountMicros: number, promotion: RechargePromotion): number {
  if (!promotion.recharge_bonus_enabled || !Number.isSafeInteger(amountMicros) || amountMicros < 1000000 || amountMicros > 10000 * 1000000 || amountMicros % 10000 !== 0) return 0;
  let threshold = 0; let percent = 0;
  for (const tier of promotion.recharge_bonus_tiers) {
    if (tier.threshold_micros >= 1000000 && amountMicros >= tier.threshold_micros && tier.threshold_micros > threshold && tier.percent >= 1 && tier.percent <= 100) {
      threshold = tier.threshold_micros; percent = tier.percent;
    }
  }
  return percent;
}

export function rechargeBonus(amountMicros: number, promotion: RechargePromotion): number {
  const percent = rechargePercent(amountMicros, promotion);
  return percent ? Math.floor(amountMicros * percent / 100) : 0;
}

// Percentage gifts can contain fractions of a cent, retained in the USD balance.
export const rechargeMoney = (micros: number) => (micros / 1000000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 6 });
