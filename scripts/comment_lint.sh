#!/usr/bin/env bash
# comment_lint.sh checks hand-written Go and Markdown text against the
# mechanical subset of WRITING.md.
#
# Rules come in two tiers.
#
#   hard      No legitimate use in a source comment. Fail the build.
#             em-dash and arrow characters, dates, PR/issue numbers, plan and
#             review labels, citations of files that do not exist in the tree,
#             and capital-letter emphasis words in Go comments.
#   advisory  Worth a reader's second look but sometimes the right word.
#             History words ("previously", "no longer"), rhetoric ("load-bearing"),
#             MUST, and comment blocks of 15+ lines. Never fail the build. The
#             point of an advisory hit is to reread the sentence, not to swap
#             the word for a synonym.
#
# Usage:
#   scripts/comment_lint.sh                 # hard rules on tracked files; exit 1 on a hit
#   scripts/comment_lint.sh --list [PATH...]
#                                           # hard + advisory hits as path:line:tier:rule:text, exit 0
#   scripts/comment_lint.sh --hard [PATH...]
#                                           # hard hits only, listed, exit 0
#
# Excluded: generated files (*.pb.go, *.gen.go, mock_*.go), WRITING.md (it
# names the banned tokens to define them), and directive lines (//go:, //nolint,
# // @swag tags).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

mode=check
paths=()
while [ $# -gt 0 ]; do
  case "$1" in
    --list) mode=list ;;
    --hard) mode=hard ;;
    *) paths+=("$1") ;;
  esac
  shift
done

files() {
  if [ ${#paths[@]} -gt 0 ]; then
    git ls-files -- "${paths[@]}"
  else
    git ls-files
  fi | grep -E "\.(go|md)$" \
     | grep -vE "(\.pb\.go|\.gen\.go|mock_[A-Za-z0-9_]*\.go)$" \
     | grep -vE "^(WRITING\.md$)"
}

# awk (mawk) has no \b, so word boundaries are spelled out: B = start or
# non-word char, E = end or non-word char. Each rule is tier|name|exts|regex,
# where exts restricts the rule to those file extensions.
B='(^|[^A-Za-z0-9_])'
E='($|[^A-Za-z0-9_])'
rules=(
  'hard|emdash|go,md|—'
  'hard|arrow|go,md|(→|⇒)'
  "hard|caps|go,md|${B}(NOT|ONLY|NEVER|ALWAYS|SAME|EVERY|EXACTLY|DO NOT)${E}"
  "hard|date|go,md|${B}(20[0-9][0-9]-[0-9][0-9](-[0-9][0-9])?|(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*[ -]20[0-9][0-9])${E}"
  "hard|pr-ref|go,md|${B}(PR ?#[0-9]+|#[0-9][0-9][0-9]+)${E}"
  "hard|label|go,md|${B}(P[0-9](\\.[0-9]+)?(-[A-Z]+)?|D-[0-9]+|design [A-Z]-?[0-9]+|\\(F[0-9]\\)|F[0-9] (review|finding|fix)|T[0-9]\\.[0-9]|Phase-? ?[0-9]+|GAP [0-9]+|SHOULD-FIX|MUST-FIX|NICE-TO-HAVE)${E}"
  "hard|review-ref|go,md|${B}([Rr]eview (finding|round|fix)|review(er)?s? (said|asked|flagged|found)|DECISIONS?(\\.md)?${E}|docs/plans/)"
  "advisory|must|go,md|${B}MUST${E}"
  "advisory|history|go,md|${B}([Pp]reviously|used to|no longer|[Ll]anded|[Ss]hipped|[Rr]everted|hotfix)${E}"
  "advisory|rhetoric|go,md|${B}(load-bearing|footgun|by construction|belt-and-braces|god-mock|the (dangerous|entire reason)|deliberately NOT|on purpose)${E}"
)

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

for rule in "${rules[@]}"; do
  tier=${rule%%|*}; rest=${rule#*|}
  name=${rest%%|*}; rest=${rest#*|}
  exts=${rest%%|*}; re=${rest#*|}
  if [ "$mode" != list ] && [ "$tier" != hard ]; then continue; fi
  files | grep -E "\.(${exts//,/|})$" | NAME="$tier:$name" RE="$re" xargs -r awk '
    BEGIN { name = ENVIRON["NAME"]; re = ENVIRON["RE"] }
    FNR==1 { md = (FILENAME ~ /\.md$/); fence = 0 }
    # Inline code spans are code, not prose.
    function strip(s) { sub(/^[[:space:]]*(\/\/|\/\*|\*)[[:space:]]?/, "", s); gsub(/`[^`]*`/, "`...`", s); gsub(/\[[^\]]*\]\([^)]*\)/, "[link]", s); return s }
    /^[[:space:]]*\/\/ Code generated .* DO NOT EDIT\.$/ { next }
    /^[[:space:]]*\/\/(go:|nolint|line |extern |export )/ { next }
    /^[[:space:]]*\/\/[[:space:]]*@[A-Za-z]/ { next }
    md && /^[[:space:]]*(```|~~~)/ { fence = !fence; next }
    md { t = strip($0); if (!fence && t ~ re) printf "%s:%d:%s:%s\n", FILENAME, FNR, name, $0; next }
    /^[[:space:]]*\/\// {
      t = strip($0)
      if (t ~ re) printf "%s:%d:%s:%s\n", FILENAME, FNR, name, t
      next
    }
    # Trailing comment after code: "x := y // reason". The marker must be
    # preceded by whitespace so "https://" inside a string does not count.
    /[[:space:]]\/\/[[:space:]]/ {
      t = $0; sub(/^.*[[:space:]]\/\/[[:space:]]/, "", t); gsub(/`[^`]*`/, "`...`", t)
      if (t ~ re) printf "%s:%d:%s:%s\n", FILENAME, FNR, name, t
    }
  ' >> "$tmp" || true
done

# hard:cite. A .md path named in a comment must be a tracked file: relative to
# the commenting file's directory, relative to the repository root, or as a
# bare basename that names exactly one tracked file. Tracked, not merely
# present, so an untracked local file cannot make a citation pass here that
# fails in CI. A code span containing a space is a command, not a citation,
# and its paths are not checked.
all_md=$(git ls-files -- "*.md")
files | xargs -r awk '
  FNR==1 { md = (FILENAME ~ /\.md$/); fence = 0 }
  md && /^[[:space:]]*(```|~~~)/ { fence = !fence; next }
  (md && !fence) || /^[[:space:]]*\/\// {
    line = $0
    gsub(/`[^`]* [^`]*`/, "``", line)
    while (match(line, /(\.\.?\/)*\.?[A-Za-z0-9_][A-Za-z0-9_.\/-]*\.md/)) {
      printf "%s:%d:%s\n", FILENAME, FNR, substr(line, RSTART, RLENGTH)
      line = substr(line, RSTART + RLENGTH)
    }
  }
' | while IFS=: read -r f n p; do
  d=$(dirname "$f")
  # RFDs live in the sibling affairs repository; those paths are not in this tree.
  case "$p" in affairs/*|*/affairs/*) continue ;; esac
  if ! git ls-files --error-unmatch -- "$d/$p" >/dev/null 2>&1 && ! git ls-files --error-unmatch -- "$p" >/dev/null 2>&1 && [ "$(grep -cE "(^|/)$p$" <<<"$all_md")" != 1 ]; then
    echo "$f:$n:hard:cite:$p does not exist in the tree"
  fi
done >> "$tmp"

if [ "$mode" = list ]; then
  files | grep -vE "\.md$" | xargs -r awk -v min=15 '
    FNR==1 { if (n>=min) printf "%s:%d:advisory:block:%d lines\n", prev, start, n; n=0 }
    /^[[:space:]]*\/\// { if (n==0) start=FNR; n++; prev=FILENAME; next }
    { if (n>=min) printf "%s:%d:advisory:block:%d lines\n", FILENAME, start, n; n=0 }
    END { if (n>=min) printf "%s:%d:advisory:block:%d lines\n", prev, start, n }
  ' >> "$tmp" || true
fi

# Key on file, line, tier and rule so two rules firing on one line both show.
sort -t: -k1,1 -k2,2n -k3,4 -u "$tmp"
hits=$(wc -l < "$tmp")

if [ "$mode" = check ] && [ "$hits" -gt 0 ]; then
  echo "comment-lint: $hits hard hit(s); see WRITING.md" >&2
  exit 1
fi
