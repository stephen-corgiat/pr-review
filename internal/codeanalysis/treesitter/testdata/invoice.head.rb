require "json"

module Billing
  RATE = 0.25

  # An invoice.
  class Invoice
    # The total, with tax.
    def total(items)
      sum = items.sum { |i| price(i) }
      helper
      sum + tax(sum)
    end

    # Price of one item.
    # Rounded.
    def price(item)
      item.amount.round(2)
    end

    def tax(x) = x * RATE

    def self.build(attrs)
      inv = new
      inv.total(attrs)
    end

    class << self
      def empty
        build([])
      end
    end

    private

    def helper
      2
    end
  end
end
