      *****************************************************************
      * The posting record: one base description of fifty bytes, and
      * three REDEFINES over two independent runs inside it.
      *
      * Every posting opens with the account key it is filed under and
      * the sequence number within that account. The type code sits
      * behind them, twelve bytes in -- so this file discriminates at
      * two different offsets, since the header and the trailer carry
      * theirs at zero.
      *
      * PST-BODY is described three ways: as itself, as PST-DEBIT and as
      * PST-CREDIT. PST-TAIL is described two ways: as itself and as
      * PST-TAIL-REF. The two runs are independent, so the alternatives
      * multiply: six record types come out of this one 01-level, and a
      * layout naming this copybook says which combination each of its
      * `record` forms means.
      *
      * PST-CREDIT is four bytes shorter than the run it redefines, which
      * is ordinary COBOL and leaves four bytes no credit posting
      * describes. A record that read those bytes carries them back out
      * unchanged -- docs/ir/SPEC.md's "Slack survives a read" -- which
      * is why a credit posting written back is byte-identical rather
      * than merely equal in its fields.
      *****************************************************************
       01  POSTING-RECORD.
           05  PST-ACCOUNT             PIC X(10).
           05  PST-SEQUENCE            PIC 9(2).
           05  PST-TYPE                PIC X(2).
           05  PST-BODY                PIC X(28).
           05  PST-DEBIT               REDEFINES PST-BODY.
               10  PDB-COST-CENTRE     PIC X(6).
               10  PDB-AMOUNT          PIC S9(11)V99 COMP-3.
               10  PDB-MEMO            PIC X(15).
           05  PST-CREDIT              REDEFINES PST-BODY.
               10  PCR-SOURCE          PIC X(4).
               10  PCR-AMOUNT          PIC S9(9)V99 COMP-3.
               10  PCR-REFERENCE       PIC X(14).
           05  PST-TAIL                PIC X(8).
           05  PST-TAIL-REF            REDEFINES PST-TAIL.
               10  PTR-BATCH           PIC 9(4).
               10  PTR-LINE            PIC 9(4).
