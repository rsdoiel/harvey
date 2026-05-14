
#
# docs.mak - Build section 7 man pages from Harvey's built-in help topics.
#
# Run "make build" first so bin/harvey exists, then:
#
#   make -f docs.mak          # regenerate all .7.md source files
#   make -f docs.mak man7     # compile .7.md sources into man/man7/
#   make -f docs.mak all      # do both
#   make -f docs.mak clean    # remove generated .7.md files and man/man7/
#
# The main Makefile's "make install" will pick up man/man7/ automatically.
#

HARVEY = ./bin/harvey

PANDOC = $(shell which pandoc)

# One entry per /help topic. Each becomes harvey-TOPIC.7.md and harvey-TOPIC.7.
MAN7_TOPICS = \
	clear \
	context \
	editing \
	files \
	file-tree \
	git \
	inspect \
	kb \
	model \
	model-alias \
	ollama \
	rag \
	read \
	read-dir \
	record \
	rename \
	routing \
	run \
	search \
	security \
	session \
	skill-set \
	skills \
	status \
	summarize \
	write

MAN7_MD  = $(addprefix harvey-,$(addsuffix .7.md,$(MAN7_TOPICS)))
MAN7_OUT = $(addprefix man/man7/harvey-,$(addsuffix .7,$(MAN7_TOPICS)))

all: docs man7

# Regenerate harvey.1.md and all topic .7.md source files from the binary.
docs: harvey.1.md $(MAN7_MD)

# Compile all topic .7.md sources to troff man pages in man/man7/.
man7: $(MAN7_OUT)

# Regenerate harvey.1.md from the main --help output.
harvey.1.md: .FORCE
	$(HARVEY) --help > $@

# Pattern rule: generate harvey-TOPIC.7.md from "harvey --help TOPIC".
harvey-%.7.md: .FORCE
	$(HARVEY) --help $* > $@

# Pattern rule: compile a .7.md source to a troff man page.
man/man7/harvey-%.7: harvey-%.7.md
	@mkdir -p man/man7
	$(PANDOC) $< --from markdown --to man -s > $@

clean:
	@for T in $(MAN7_TOPICS); do \
		if [ -f "harvey-$${T}.7.md" ]; then rm "harvey-$${T}.7.md"; fi; \
	done
	@if [ -d man/man7 ]; then rm -fR man/man7; fi

.FORCE:
