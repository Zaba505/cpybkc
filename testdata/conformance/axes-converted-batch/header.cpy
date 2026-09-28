      *****************************************************************
      * The record that opens a batch.
      *
      * Sixteen bytes: the type code every record of this file carries in
      * its first byte, and the batch's name. The type code is what the
      * layout discriminates on, and it is an item of the record like any
      * other -- a framing byte would belong to the dataset instead.
      *****************************************************************
       01  BATCH-HEADER.
           05  HDR-TYPE                PIC X(1).
           05  HDR-NAME                PIC X(15).
