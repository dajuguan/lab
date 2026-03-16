// run: quint verify --temporal=eventually_finished  gcd.qnt
module gcd {
  /// Mutable state used by the subtraction-based Euclidean algorithm.
  ///
  /// This concrete version fixes the initial values to avoid a Quint 0.31.0
  /// front-end crash when temporal properties are combined with const params.
  var x: int
  var y: int

  action init = all {
    x' = 48,
    y' = 18,
  }

  action subtract_x = all {
    x > y,
    x' = x - y,
    y' = y,
  }

  action subtract_y = all {
    y > x,
    x' = x,
    y' = y - x,
  }

  action done = all {
    x == y,
    x' = x,
    y' = y,
  }

  /// One algorithm step: subtract the smaller value from the larger one.
  action step = any {
    subtract_x,
    subtract_y,
    done,
  }

  /// Both working values remain positive throughout execution.
  val positives = x > 0 and y > 0

  /// The algorithm terminates when both values become equal.
  val finished = x == y

  /// The result produced by the algorithm once finished.
  val result = x

  /// For the concrete input 48 and 18, the gcd should be 6 when the algorithm finishes.
  val result_correct = finished implies result == 6

  /// The subtraction-based algorithm eventually reaches a finished state.
  temporal eventually_finished = eventually(finished)
}


