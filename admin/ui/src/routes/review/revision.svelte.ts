// A counter every review action bumps: the header chip, the Publish state, the Review tab and the History
// activity all reload their review data when it changes, without reloading the item (no discard prompt).
export const reviewRev = $state({ n: 0 });

export const bumpReview = () => {
  reviewRev.n++;
};
