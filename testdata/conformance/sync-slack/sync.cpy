      *****************************************************************
      * The record this entry is about: one whose SYNCHRONIZED items
      * force bytes that belong to no item at all.
      *
      * Twenty bytes, and only sixteen of them are covered by an item.
      * A SYNCHRONIZED item aligns against the start of the record
      * containing it, so the compiler inserts slack ahead of one that
      * would otherwise begin off its boundary:
      *
      *   SYN-KEY       X(3)               offset  0, 3 bytes
      *   ..slack..                        offset  3, 1 byte
      *   SYN-SEQ       S9(4) COMP SYNC    offset  4, 2 bytes
      *   SYN-CODE      X(3)               offset  6, 3 bytes
      *   ..slack..                        offset  9, 3 bytes
      *   SYN-AMOUNT    S9(9) COMP SYNC    offset 12, 4 bytes
      *   SYN-TAIL      X(4)               offset 16, 4 bytes
      *
      * SYN-SEQ is a halfword and begins on an even offset; SYN-CODE
      * leaves the next byte at 9 and SYN-AMOUNT is a fullword, so it
      * begins at 12. Neither gap is anything a program wrote.
      *
      * Every item is on one side or the other of a run of slack, and
      * SYN-TAIL is behind both of them: a consumer that leaves either
      * run out reads SYN-AMOUNT and SYN-TAIL from bytes belonging to
      * something else, and one that leaves both out reads the second
      * record of the file four bytes early.
      *
      * The two runs are deliberately of different widths. A consumer
      * that retains them but pairs them to the wrong nodes hands the
      * writer three bytes where one is wanted, which docs/ir/SPEC.md's
      * "Slack survives a read" makes an error a writer reports rather
      * than one it pads away.
      *****************************************************************
       01  SYNC-RECORD.
           05  SYN-KEY                 PIC X(3).
           05  SYN-SEQ                 PIC S9(4) COMP SYNC.
           05  SYN-CODE                PIC X(3).
           05  SYN-AMOUNT              PIC S9(9) COMP SYNC.
           05  SYN-TAIL                PIC X(4).
