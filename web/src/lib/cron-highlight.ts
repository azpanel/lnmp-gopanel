import Prism from "prismjs";
import "prismjs/components/prism-bash";

// React renders the tokens; Prism must not mutate its DOM in the background.
Prism.manual = true;

const schedule = /^[\t ]*(?:@\w+|(?:\S+[\t ]+){4}\S+)(?=[\t ]+\S)/m;
const grammar: Prism.Grammar = {
  comment: /^[\t ]*#.*/m,
  environment: {
    pattern: /^[\t ]*[A-Za-z_]\w*[\t ]*=.*/m,
    inside: Prism.languages.bash,
  },
  task: {
    pattern: /^[\t ]*(?:@\w+|(?:\S+[\t ]+){4}\S+)[\t ]+[^\r\n]+/m,
    inside: {
      schedule: { pattern: schedule, greedy: true, alias: "keyword" },
      rest: Prism.languages.bash,
    },
  },
};

export function tokenizeCrontab(raw: string) {
  return Prism.tokenize(raw, grammar);
}
