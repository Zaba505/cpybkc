      *****************************************************************
      * Zoned decimal items in EBCDIC.
      *
      * Twenty-seven bytes. The leading overpunch is the one position with no
      * ASCII row in Appendix A, and it is here rather than in the ASCII entry
      * for that reason.
      *****************************************************************
       01  ZONED-RECORD.
           05  Z-SIGNED                PIC S9(5).
           05  Z-UNSIGNED              PIC 9(5).
           05  Z-LEAD-SEP              PIC S9(5) SIGN LEADING SEPARATE.
           05  Z-TRAIL-SEP             PIC S9(5) SIGN TRAILING SEPARATE.
           05  Z-LEAD-OVER             PIC S9(5) SIGN LEADING.
