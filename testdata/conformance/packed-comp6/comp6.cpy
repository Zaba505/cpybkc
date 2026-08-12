      *****************************************************************
      * COMP-6 items covering Appendix A.4's two COMP-6 rows.
      *
      * Seven bytes. COMP-6 is one nibble per digit and no sign nibble, so
      * C6-EVEN is two bytes where PIC 9(4) COMP-3 would be three, and C6-ODD
      * is two as well: three digits and the pad nibble that an odd digit
      * count carries in place of the sign nibble COMP-3 would have put there.
      *
      * Neither item is signed, and neither may be: there is nowhere in a
      * COMP-6 field for a sign to go.
      *
      * C6-TRAILER is the item this entry is really about. An item read with
      * an accessor that expects a sign nibble consumes ceil((digits+1)/2)
      * bytes rather than ceil(digits/2), and codec's reader is positional —
      * so a byte taken here is a byte taken off the front of whatever follows.
      *****************************************************************
       01  COMP6-RECORD.
           05  C6-EVEN                 PIC 9(4) COMP-6.
           05  C6-ODD                  PIC 9(3) COMP-6.
           05  C6-TRAILER              PIC X(3).
