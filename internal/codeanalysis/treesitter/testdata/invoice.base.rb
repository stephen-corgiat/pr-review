require "json"

module Billing
  # An invoice.
  class Invoice
    # The total, with tax.
    def total(items)
      sum = items.sum { |i| price(i) }
      sum + tax(sum)
    end

    # Price of one item.
    def price(item)
      item.amount
    end

    def tax(x) = x * 0.2

    def self.build(attrs)
      new(attrs)
    end

    private

    def helper
      1
    end
  end
end
