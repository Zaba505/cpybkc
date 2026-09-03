      *****************************************************************
      * The record that opens a batch, keyed on a packed item.
      *
      * Eight bytes: a packed key at the front and a name behind it. The
      * key is what the layout discriminates on, and it sits at bytes
      * zero to two -- the three bytes a detail's own packed amount
      * occupies.
      *****************************************************************
       01  BATCH-HEADER.
           05  BH-KEY                  PIC S9(5) COMP-3.
           05  BH-NAME                 PIC X(5).
