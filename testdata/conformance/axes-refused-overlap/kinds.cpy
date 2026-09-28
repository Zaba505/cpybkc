      *****************************************************************
      * The two records of the axes-refused-overlap entry.
      *
      * Five bytes each: a one-digit signed zoned kind and a four-byte
      * note. UNSIGNED-KIND is told apart by F5 and POSITIVE-KIND by C5,
      * which are two bytes under the ebcdic convention.
      *****************************************************************
       01  UNSIGNED-RECORD.
           05  UNSIGNED-KIND           PIC S9.
           05  UNSIGNED-NOTE           PIC X(4).
       01  POSITIVE-RECORD.
           05  POSITIVE-KIND           PIC S9.
           05  POSITIVE-NOTE           PIC X(4).
